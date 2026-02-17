/**
 * SDK Version
 * Read from package.json at module initialization
 */
function getSDKVersion(): string {
  try {
    // eslint-disable-next-line @typescript-eslint/no-require-imports, @typescript-eslint/no-var-requires
    return require('../package.json').version;
  } catch {
    return '0.0.0'; // Fallback if package.json is not accessible
  }
}

export const VERSION = getSDKVersion();

/**
 * Logger interface for custom logging implementations.
 * Allows users to inject their own logger (e.g., Winston, Pino, Bunyan).
 *
 * Example with custom logger:
 * ```ts
 * const client = new HeimdallClient({
 *   target: 'localhost:50051',
 *   logger: {
 *     debug: (msg) => logger.debug(msg),
 *     info: (msg) => logger.info(msg),
 *     warn: (msg) => logger.warn(msg),
 *     error: (msg) => logger.error(msg),
 *   },
 * });
 * ```
 */
export interface Logger {
  debug(message: string): void;
  info(message: string): void;
  warn(message: string): void;
  error(message: string): void;
}

/**
 * Represents the contextual data used to evaluate feature flags.
 * Keys are attribute names (e.g., "user_id", "role") and values are strings.
 *
 * Example:
 * ```ts
 * const ctx: Context = {
 * user_id: "u-123",
 * plan: "enterprise",
 * region: "us-east-1"
 * };
 * ```
 */
export type Context = Record<string, string>;

/**
 * Result of a flag evaluation.
 */
export interface EvaluationResult {
  /**
   * The evaluated flag value (true = enabled, false = disabled).
   */
  value: boolean;

  /**
   * Reason explains why the decision was made.
   * Examples: "STATIC_OFF", "DEFAULT_VALUE", "RULE_MATCH".
   * Critical for debugging why a specific value was returned.
   */
  reason: string;
}

/**
 * Creates an EvaluationResult from a gRPC response.
 * Unpacks the response and instantiates the proper EvaluationResult object.
 * Single source of truth for response → EvaluationResult conversion.
 *
 * @param response - The gRPC EvaluateResponse (may be null/undefined)
 * @param err - Any error that occurred during the gRPC call (gRPC errors are not strictly typed)
 * @param defaultValue - The default value to return on error
 * @returns An EvaluationResult object
 */
export function fromEvaluateResponse(
  response: unknown,
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  err: any,
  defaultValue: boolean,
): EvaluationResult {
  if (err || !response) {
    return { value: defaultValue, reason: 'ERROR' };
  }
  return {
    value: (response as Record<string, unknown>).value as boolean,
    reason: (response as Record<string, unknown>).reason as string,
  };
}

/**
 * Configuration options for the Heimdall Client.
 */
export interface HeimdallOptions {
  /**
   * The gRPC server address (e.g., "localhost:50051" or "heimdall.internal:80").
   * MUST be a non-empty string.
   */
  target: string;

  /**
   * The API Key used to authenticate with the Heimdall server.
   * MUST be a non-empty string.
   */
  apiKey: string;

  /**
   * Request timeout in milliseconds.
   *
   * Constraints:
   * - Min: 20ms (to prevent network jitter failures).
   * - Max: 10000ms (10 seconds) - Hard limit to prevent resource exhaustion.
   * - Default: 5000ms.
   */
  timeout?: number;

  /**
   * Time-to-Live (TTL) for the internal L1 cache in milliseconds.
   * This defines how long a flag evaluation result remains valid in memory.
   *
   * - Default: 60000ms (1 minute).
   * - Set to 0 to disable caching.
   */
  cacheTTL?: number;

  /**
   * Maximum number of items to hold in the L1 cache.
   * Prevents memory leaks in the client application by evicting the least recently used items.
   *
   * - Default: 1000 items.
   */
  cacheSize?: number;

  /**
   * Use insecure credentials (plaintext).
   * Set to `true` when connecting to localhost or inside a private network without TLS.
   *
   * @defaultValue false
   */
  insecure?: boolean;

  /**
   * Custom logger instance for controlling log output.
   * If not provided, uses default logger with info/warn/error levels.
   * Debug logs are disabled by default (no-op).
   *
   * @defaultValue Default logger (console-based)
   */
  logger?: Logger;
}
