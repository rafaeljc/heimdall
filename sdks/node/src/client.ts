import * as grpc from '@grpc/grpc-js';
import { LRUCache } from 'lru-cache';
import { DataPlaneServiceClient, EvaluateRequest } from './generated/heimdall/v1/data_plane';
import { Context, HeimdallOptions, Logger, VERSION } from './types';

// =============================================================================
// Constants & Defaults (Safety Limits)
// =============================================================================
const DEFAULT_TIMEOUT_MS = 5000;
const MIN_TIMEOUT_MS = 20;
const MAX_TIMEOUT_MS = 10000; // 10s hard limit

const DEFAULT_CACHE_TTL_MS = 60_000; // 1 minute
const DEFAULT_CACHE_SIZE = 1000;

/**
 * HeimdallClient is the high-performance, gRPC-based Node.js client for Heimdall.
 * It handles connection management, L1 caching, and fault tolerance.
 */
export class HeimdallClient {
  private client: DataPlaneServiceClient;
  private readonly timeout: number;
  private readonly logger: Logger;

  // L1 Cache: Stores evaluation results to reduce network calls
  private readonly cache: LRUCache<string, boolean> | null;

  /**
   * Creates a new instance of HeimdallClient.
   *
   * @param options - Configuration options for the client.
   * @throws {Error} If the target is missing or invalid.
   */
  constructor(options: HeimdallOptions) {
    this.logger = options.logger ?? this.createDefaultLogger();
    this.validateTarget(options.target);
    this.timeout = this.sanitizeTimeout(options.timeout);

    // Initialize Cache Strategy
    const ttl = options.cacheTTL ?? DEFAULT_CACHE_TTL_MS;
    const max = options.cacheSize ?? DEFAULT_CACHE_SIZE;

    if (ttl > 0) {
      this.cache = new LRUCache({
        max,
        ttl,
        updateAgeOnGet: false, // Strict TTL compliance (expiry based on fetch time)
      });
      this.logger.debug(`Cache enabled: max=${max} items, ttl=${ttl}ms`);
    } else {
      this.cache = null;
      this.logger.debug('Cache disabled');
    }

    // Initialize gRPC Credentials
    const credentials = options.insecure
      ? grpc.credentials.createInsecure()
      : grpc.credentials.createSsl();

    // Initialize the generated Client
    this.client = new DataPlaneServiceClient(options.target, credentials);
    this.logger.info(`Connected to Heimdall at ${options.target}`);
  }

  /**
   * Creates a default logger instance with console-based output.
   */
  private createDefaultLogger(): Logger {
    const prefix = `[Heimdall SDK v${VERSION}]`;
    return {
      debug: () => {}, // No-op by default. Custom loggers can handle lazy evaluation.
      // eslint-disable-next-line no-console
      info: (msg) => console.log(`${prefix} ${msg}`),
      // eslint-disable-next-line no-console
      warn: (msg) => console.warn(`${prefix} ${msg}`),
      // eslint-disable-next-line no-console
      error: (msg) => console.error(`${prefix} ${msg}`),
    };
  }

  /**
   * Validates that the target connection string is present.
   */
  private validateTarget(target: string): void {
    if (!target || target.trim().length === 0) {
      throw new Error(
        '[Heimdall SDK] Configuration Error: "target" is required and cannot be empty.',
      );
    }
  }

  /**
   * Sanitizes the timeout value to ensure it stays within safe operational bounds.
   */
  private sanitizeTimeout(timeout?: number): number {
    if (timeout === undefined || timeout === null) return DEFAULT_TIMEOUT_MS;

    // Check for NaN or non-number types
    if (typeof timeout !== 'number' || isNaN(timeout)) {
      this.logger.warn(
        `Invalid timeout provided (${timeout}). Using default ${DEFAULT_TIMEOUT_MS}ms.`,
      );
      return DEFAULT_TIMEOUT_MS;
    }

    // Clamp values to prevent misconfiguration that could lead to resource exhaustion or immediate failures.
    if (timeout < MIN_TIMEOUT_MS) {
      this.logger.warn(
        `Timeout ${timeout}ms is below minimum ${MIN_TIMEOUT_MS}ms. Clamping to ${MIN_TIMEOUT_MS}ms.`,
      );
      return MIN_TIMEOUT_MS;
    }
    if (timeout > MAX_TIMEOUT_MS) {
      this.logger.warn(
        `Timeout ${timeout}ms exceeds maximum ${MAX_TIMEOUT_MS}ms. Clamping to ${MAX_TIMEOUT_MS}ms.`,
      );
      return MAX_TIMEOUT_MS;
    }

    return timeout;
  }

  /**
   * Generates a deterministic cache key.
   * Sorts context keys to ensure {a:1, b:2} hits the same cache as {b:2, a:1}.
   */
  private generateCacheKey(key: string, context: Context): string {
    if (Object.keys(context).length === 0) {
      return `flag:${key}`;
    }

    const sortedContext = Object.keys(context)
      .sort()
      .map((k) => `${k}:${context[k]}`)
      .join('|');

    return `flag:${key}|ctx:${sortedContext}`;
  }

  /**
   * Evaluates a boolean feature flag.
   *
   * Uses a read-through caching strategy:
   * 1. Check L1 in-memory cache first
   * 2. On miss, fetch from gRPC server
   * 3. Cache the result for future calls
   * 4. Return the value or default on error
   *
   * @param key - The unique identifier of the feature flag
   * @param context - User/request context for rule evaluation (e.g., user_id, region)
   * @param defaultValue - Fallback value returned if evaluation fails or server is unavailable
   * @returns The evaluated flag value from cache or server; defaults to `defaultValue` on error
   */
  public async getBool(key: string, context: Context, defaultValue: boolean): Promise<boolean> {
    // 1. Fast Path: Cache Hit
    if (this.cache) {
      const cacheKey = this.generateCacheKey(key, context);
      const cachedValue = this.cache.get(cacheKey);
      if (cachedValue !== undefined) {
        this.logger.debug(`Cache hit for flag '${key}'`);
        return cachedValue;
      }
      this.logger.debug(`Cache miss for flag '${key}'`);
    }

    // 2. Slow Path: Network Call
    return this.fetchFromNetwork(key, context, defaultValue);
  }

  /**
   * Performs the actual gRPC call with deadline enforcement.
   *
   * This is the slow path, called only on cache misses.
   * Handles server errors gracefully by returning the default value
   * without caching (to allow retries on transient failures).
   *
   * @param key - Feature flag key
   * @param context - Evaluation context
   * @param defaultValue - Value to return on error
   * @returns The flag value from server, or defaultValue on error
   * @private
   */
  private async fetchFromNetwork(
    key: string,
    context: Context,
    defaultValue: boolean,
  ): Promise<boolean> {
    return new Promise((resolve) => {
      // Construct the Request POJO (Plain Old JavaScript Object)
      // We map the incoming 'key' to the proto definition 'flagKey'
      const request: EvaluateRequest = {
        flagKey: key,
        context: context,
      };

      // Set Deadline (Client-side Timeout)
      const deadline = new Date(Date.now() + this.timeout);
      const metadata = new grpc.Metadata();

      this.logger.debug(`Evaluating flag '${key}' with context: ${JSON.stringify(context)}`);

      // Execute gRPC Call
      this.client.evaluate(request, metadata, { deadline }, (err, response) => {
        if (err) {
          // Log the error but return default to keep application alive.
          // We do NOT cache errors (so we can retry later).
          this.logger.error(
            `Failed to evaluate flag '${key}': ${err.code} - ${err.message}. Returning default: ${defaultValue}`,
          );
          resolve(defaultValue);
          return;
        }

        if (!response) {
          this.logger.warn(
            `Empty response received for flag '${key}'. Returning default: ${defaultValue}`,
          );
          resolve(defaultValue);
          return;
        }

        const value = response.value;
        this.logger.debug(`Flag '${key}' evaluated to: ${value}`);

        // 3. Write to Cache (if enabled)
        if (this.cache) {
          const cacheKey = this.generateCacheKey(key, context);
          this.cache.set(cacheKey, value);
          this.logger.debug(`Cached result for flag '${key}'`);
        }

        resolve(value);
      });
    });
  }

  /**
   * Closes the gRPC client connection and clears any pending resources.
   *
   * **Important**: Call this method during application shutdown to ensure
   * graceful connection closure and prevent resource leaks.
   *
   * @example
   * ```typescript
   * const client = new HeimdallClient({ target: 'localhost:50051' });
   * // ... use client ...
   * client.close(); // On shutdown (e.g., in process.on('SIGTERM'))
   * ```
   */
  public close(): void {
    this.client.close();
    if (this.cache) {
      this.cache.clear();
    }
  }
}
