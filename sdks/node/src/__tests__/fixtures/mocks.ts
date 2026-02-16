import { Logger, Context } from '../../types';

/**
 * Creates a mock Logger for testing purposes.
 * Tracks all log calls for assertion in tests.
 */
export function createMockLogger(): Logger & { logs: Record<string, string[]> } {
  const logs: Record<string, string[]> = {
    debug: [],
    info: [],
    warn: [],
    error: [],
  };

  return {
    debug: (msg: string) => logs.debug.push(msg),
    info: (msg: string) => logs.info.push(msg),
    warn: (msg: string) => logs.warn.push(msg),
    error: (msg: string) => logs.error.push(msg),
    logs,
  };
}

/**
 * Sample feature flag contexts for testing.
 */
export const MOCK_CONTEXTS = {
  basic: { user_id: 'user123' } as Context,
  multiField: { user_id: 'user456', region: 'us-west-2', tier: 'premium' } as Context,
  empty: {} as Context,
};

/**
 * Mock gRPC responses for typical scenarios.
 */
export const MOCK_RESPONSES = {
  flagEnabled: { value: true },
  flagDisabled: { value: false },
};
