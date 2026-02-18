import * as grpc from '@grpc/grpc-js';
import {
  DataPlaneServiceService,
  DataPlaneServiceServer,
  EvaluateRequest,
  EvaluateResponse,
} from '../../../src/generated/heimdall/v1/data_plane';

/**
 * Real gRPC server for integration testing.
 * Implements the actual Heimdall DataPlaneService using generated protobuf types.
 * This allows testing actual flag evaluation with proper protobuf serialization.
 */
export class MockDataPlaneServer {
  private server: grpc.Server;
  private port: number;

  // Configurable behavior for different test scenarios
  private shouldFail = false;
  private evaluationResults: Record<string, boolean> = {};

  // Track metadata received in requests for testing
  private lastReceivedMetadata: grpc.Metadata | null = null;

  constructor(port: number = 50051) {
    this.port = port;
    this.server = new grpc.Server();
  }

  /**
   * Starts the real gRPC server using the generated DataPlaneService definition.
   */
  async start(): Promise<void> {
    const implementation: DataPlaneServiceServer = {
      evaluate: (
        call: grpc.ServerUnaryCall<EvaluateRequest, EvaluateResponse>,
        callback: grpc.sendUnaryData<EvaluateResponse>,
      ): void => {
        // Capture metadata for testing
        this.lastReceivedMetadata = call.metadata;

        try {
          if (this.shouldFail) {
            const error: grpc.ServiceError = {
              name: 'ServiceError',
              message: 'Simulated server error',
              code: grpc.status.INTERNAL,
              details: 'Simulated failure mode enabled',
              metadata: new grpc.Metadata(),
            };
            callback(error, null);
            return;
          }

          const request = call.request as EvaluateRequest;
          const flagKey = request.flagKey || '';
          const value = this.evaluationResults[flagKey] ?? false;

          const response: EvaluateResponse = {
            value,
            reason: value ? 'RULE_MATCH' : 'DEFAULT_VALUE',
          };

          callback(null, response);
        } catch (err) {
          const error: grpc.ServiceError = {
            name: 'ServiceError',
            message: (err as Error).message || 'Unknown error',
            code: grpc.status.UNKNOWN,
            details: 'Handler exception',
            metadata: new grpc.Metadata(),
          };
          callback(error, null);
        }
      },
    };

    this.server.addService(DataPlaneServiceService, implementation);

    return new Promise((resolve, reject) => {
      this.server.bindAsync(
        `127.0.0.1:${this.port}`,
        grpc.ServerCredentials.createInsecure(),
        (err) => {
          if (err) reject(err);
          else resolve();
        },
      );
    });
  }

  /**
   * Stops the mock server gracefully.
   */
  async stop(): Promise<void> {
    return new Promise((resolve) => {
      this.server.tryShutdown(() => resolve());
    });
  }

  /**
   * Configures the evaluation result for a specific flag.
   */
  setEvaluationResult(flagKey: string, value: boolean): void {
    this.evaluationResults[flagKey] = value;
  }

  /**
   * Configures the server to fail on next request.
   */
  setFailureMode(shouldFail: boolean): void {
    this.shouldFail = shouldFail;
  }

  /**
   * Resets all configuration to defaults.
   */
  reset(): void {
    this.shouldFail = false;
    this.evaluationResults = {};
    this.lastReceivedMetadata = null;
  }

  /**
   * Gets the server address (for client connections).
   */
  getAddress(): string {
    return `127.0.0.1:${this.port}`;
  }

  /**
   * Gets the last metadata received from a request (for testing).
   */
  getLastMetadata(): grpc.Metadata | null {
    return this.lastReceivedMetadata;
  }
}
