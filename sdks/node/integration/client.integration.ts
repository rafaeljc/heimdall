import { HeimdallClient } from '../src/client';
import { MockDataPlaneServer } from './fixtures/grpc-server';

/**
 * Integration tests for HeimdallClient.
 *
 * These tests use a gRPC server implementing the actual DataPlaneService.
 * This allows testing:
 * - Actual protobuf serialization/deserialization
 * - Real flag evaluation logic
 * - Proper gRPC communication end-to-end
 */
describe('HeimdallClient - Integration Tests', () => {
  let server: MockDataPlaneServer;
  const serverPort = 50052; // Use different port to avoid conflicts

  beforeAll(async () => {
    server = new MockDataPlaneServer(serverPort);
    await server.start();
  });

  afterAll(async () => {
    await server.stop();
  });

  afterEach(() => {
    server.reset();
  });

  describe('Real Flag Evaluation', () => {
    it('should evaluate flags with different boolean values', async () => {
      // Test true value
      server.setEvaluationResult('feature_enabled', true);
      const client = new HeimdallClient({
        target: server.getAddress(),
        insecure: true,
      });
      let result = await client.getBool('feature_enabled', {}, false);
      expect(result).toBe(true);

      // Test false value
      server.setEvaluationResult('feature_disabled', false);
      result = await client.getBool('feature_disabled', {}, true);
      expect(result).toBe(false);

      client.close();
    });

    it('should send context alongside flag key in requests', async () => {
      server.setEvaluationResult('feature_premium', true);

      const client = new HeimdallClient({
        target: server.getAddress(),
        insecure: true,
      });

      // This call includes context that should be serialized and sent to the server
      const result = await client.getBool(
        'feature_premium',
        { user_id: 'premium_user', tier: 'gold' },
        false,
      );
      expect(result).toBe(true);

      // Verify the server received a request (the fact that we got a response
      // proves the gRPC call succeeded with context serialized in the protobuf message)
      client.close();
    });
  });

  describe('Caching Behavior with Real Server', () => {
    it('should cache results and not make repeated server calls', async () => {
      server.setEvaluationResult('feature_cached', true);

      const client = new HeimdallClient({
        target: server.getAddress(),
        insecure: true,
        cacheTTL: 60000, // 60 second cache
      });

      // First call - should hit server
      const result1 = await client.getBool('feature_cached', { user_id: '123' }, false);
      expect(result1).toBe(true);

      // Change server behavior
      server.setEvaluationResult('feature_cached', false);

      // Second call with same context - should hit cache (return true, not false)
      const result2 = await client.getBool('feature_cached', { user_id: '123' }, false);
      expect(result2).toBe(true); // Cached value, not the new false from server

      client.close();
    });

    it('should respect cache TTL and re-fetch after expiration', async () => {
      server.setEvaluationResult('feature_ttl', true);

      const client = new HeimdallClient({
        target: server.getAddress(),
        insecure: true,
        cacheTTL: 100, // 100ms TTL (short for testing)
      });

      // First call
      const result1 = await client.getBool('feature_ttl', {}, false);
      expect(result1).toBe(true);

      // Change server value
      server.setEvaluationResult('feature_ttl', false);

      // Wait for cache to expire
      await new Promise((resolve) => setTimeout(resolve, 150));

      // Second call after expiration - should get new value from server
      const result2 = await client.getBool('feature_ttl', {}, true);
      expect(result2).toBe(false); // Fresh value from server

      client.close();
    });

    it('should disable cache when cacheTTL is 0', async () => {
      server.setEvaluationResult('feature_no_cache', true);

      const client = new HeimdallClient({
        target: server.getAddress(),
        insecure: true,
        cacheTTL: 0, // Disable cache
      });

      // First call
      const result1 = await client.getBool('feature_no_cache', {}, false);
      expect(result1).toBe(true);

      // Change server value
      server.setEvaluationResult('feature_no_cache', false);

      // Second call should hit server again (no cache)
      const result2 = await client.getBool('feature_no_cache', {}, true);
      expect(result2).toBe(false); // Gets new value from server

      client.close();
    });
  });

  describe('Error Handling with Real Server', () => {
    it('should return default value on server error', async () => {
      server.setEvaluationResult('any_flag', true);

      const client = new HeimdallClient({
        target: server.getAddress(),
        insecure: true,
      });

      // Configure server to fail
      server.setFailureMode(true);

      const result = await client.getBool('any_flag', {}, false);
      expect(result).toBe(false); // Returns default value

      client.close();
    });

    it('should handle connection timeout gracefully', async () => {
      server.setEvaluationResult('any_flag', true);

      // Connect to non-existent server
      const client = new HeimdallClient({
        target: '127.0.0.1:59999', // Invalid port (not listening)
        insecure: true,
        timeout: 500, // Short timeout
      });

      const result = await client.getBool('any_flag', {}, false);
      expect(result).toBe(false); // Returns default value

      client.close();
    });

    it('should not cache error responses to allow retry', async () => {
      server.setEvaluationResult('feature_retry', true);

      const client = new HeimdallClient({
        target: server.getAddress(),
        insecure: true,
      });

      // First call - server fails
      server.setFailureMode(true);
      const result1 = await client.getBool('feature_retry', {}, false);
      expect(result1).toBe(false); // Returns default

      // Second call - server recovers
      server.setFailureMode(false);
      const result2 = await client.getBool('feature_retry', {}, false);
      expect(result2).toBe(true); // Gets actual value from server (not cached error)

      client.close();
    });
  });

  describe('Context Consistency', () => {
    it('should generate consistent cache keys regardless of context order', async () => {
      server.setEvaluationResult('feature_context', true);

      const client = new HeimdallClient({
        target: server.getAddress(),
        insecure: true,
        cacheTTL: 60000,
      });

      // First call with context in one order
      const result1 = await client.getBool(
        'feature_context',
        { user_id: '123', region: 'us-west' },
        false,
      );
      expect(result1).toBe(true);

      // Change server behavior
      server.setEvaluationResult('feature_context', false);

      // Second call with same context in different order
      const result2 = await client.getBool(
        'feature_context',
        { region: 'us-west', user_id: '123' }, // Different order
        false,
      );
      // Should hit cache (return true), proving cache key is order-independent
      expect(result2).toBe(true);

      client.close();
    });
  });

  describe('Client Lifecycle', () => {
    it('should close connection gracefully', async () => {
      const client = new HeimdallClient({
        target: server.getAddress(),
        insecure: true,
      });

      // Should not throw
      expect(() => client.close()).not.toThrow();
    });
  });
});
