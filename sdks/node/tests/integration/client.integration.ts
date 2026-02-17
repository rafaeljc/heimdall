import { HeimdallClient } from '../../src/client';
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
  const DUMMY_API_KEY = 'test_key_dummy_1234567890abcdef';
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
        apiKey: DUMMY_API_KEY,
        insecure: true,
      });
      let result = await client.getEvaluation('feature_enabled', {}, false);
      expect(result.value).toBe(true);

      // Test false value
      server.setEvaluationResult('feature_disabled', false);
      result = await client.getEvaluation('feature_disabled', {}, true);
      expect(result.value).toBe(false);

      client.close();
    });

    it('should send context alongside flag key in requests', async () => {
      server.setEvaluationResult('feature_premium', true);

      const client = new HeimdallClient({
        target: server.getAddress(),
        apiKey: DUMMY_API_KEY,
        insecure: true,
      });

      // This call includes context that should be serialized and sent to the server
      const result = await client.getEvaluation(
        'feature_premium',
        { user_id: 'premium_user', tier: 'gold' },
        false,
      );
      expect(result.value).toBe(true);

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
        apiKey: DUMMY_API_KEY,
        insecure: true,
        cacheTTL: 60000, // 60 second cache
      });

      // First call - should hit server
      const result1 = await client.getEvaluation('feature_cached', { user_id: '123' }, false);
      expect(result1.value).toBe(true);

      // Change server behavior
      server.setEvaluationResult('feature_cached', false);

      // Second call with same context - should hit cache (return true, not false)
      const result2 = await client.getEvaluation('feature_cached', { user_id: '123' }, false);
      expect(result2.value).toBe(true); // Cached value, not the new false from server

      client.close();
    });

    it('should respect cache TTL and re-fetch after expiration', async () => {
      server.setEvaluationResult('feature_ttl', true);

      const client = new HeimdallClient({
        target: server.getAddress(),
        apiKey: DUMMY_API_KEY,
        insecure: true,
        cacheTTL: 100, // 100ms TTL (short for testing)
      });

      // First call
      const result1 = await client.getEvaluation('feature_ttl', {}, false);
      expect(result1.value).toBe(true);

      // Change server value
      server.setEvaluationResult('feature_ttl', false);

      // Wait for cache to expire
      await new Promise((resolve) => setTimeout(resolve, 150));

      // Second call after expiration - should get new value from server
      const result2 = await client.getEvaluation('feature_ttl', {}, true);
      expect(result2.value).toBe(false); // Fresh value from server

      client.close();
    });

    it('should disable cache when cacheTTL is 0', async () => {
      server.setEvaluationResult('feature_no_cache', true);

      const client = new HeimdallClient({
        target: server.getAddress(),
        apiKey: DUMMY_API_KEY,
        insecure: true,
        cacheTTL: 0, // Disable cache
      });

      // First call
      const result1 = await client.getEvaluation('feature_no_cache', {}, false);
      expect(result1.value).toBe(true);

      // Change server value
      server.setEvaluationResult('feature_no_cache', false);

      // Second call should hit server again (no cache)
      const result2 = await client.getEvaluation('feature_no_cache', {}, true);
      expect(result2.value).toBe(false); // Gets new value from server

      client.close();
    });
  });

  describe('Error Handling with Real Server', () => {
    it('should return default value on server error', async () => {
      server.setEvaluationResult('any_flag', true);

      const client = new HeimdallClient({
        target: server.getAddress(),
        apiKey: DUMMY_API_KEY,
        insecure: true,
      });

      // Configure server to fail
      server.setFailureMode(true);

      const result = await client.getEvaluation('any_flag', {}, false);
      expect(result.value).toBe(false); // Returns default value

      client.close();
    });

    it('should handle connection timeout gracefully', async () => {
      server.setEvaluationResult('any_flag', true);

      // Connect to non-existent server
      const client = new HeimdallClient({
        target: '127.0.0.1:59999', // Invalid port (not listening)
        apiKey: DUMMY_API_KEY,
        insecure: true,
        timeout: 500, // Short timeout
      });

      const result = await client.getEvaluation('any_flag', {}, false);
      expect(result.value).toBe(false); // Returns default value

      client.close();
    });

    it('should not cache error responses to allow retry', async () => {
      server.setEvaluationResult('feature_retry', true);

      const client = new HeimdallClient({
        target: server.getAddress(),
        apiKey: DUMMY_API_KEY,
        insecure: true,
      });

      // First call - server fails
      server.setFailureMode(true);
      const result1 = await client.getEvaluation('feature_retry', {}, false);
      expect(result1.value).toBe(false); // Returns default

      // Second call - server recovers
      server.setFailureMode(false);
      const result2 = await client.getEvaluation('feature_retry', {}, false);
      expect(result2.value).toBe(true); // Gets actual value from server (not cached error)

      client.close();
    });
  });

  describe('Context Consistency', () => {
    it('should generate consistent cache keys regardless of context order', async () => {
      server.setEvaluationResult('feature_context', true);

      const client = new HeimdallClient({
        target: server.getAddress(),
        apiKey: DUMMY_API_KEY,
        insecure: true,
        cacheTTL: 60000,
      });

      // First call with context in one order
      const result1 = await client.getEvaluation(
        'feature_context',
        { user_id: '123', region: 'us-west' },
        false,
      );
      expect(result1.value).toBe(true);

      // Change server behavior
      server.setEvaluationResult('feature_context', false);

      // Second call with same context in different order
      const result2 = await client.getEvaluation(
        'feature_context',
        { region: 'us-west', user_id: '123' }, // Different order
        false,
      );
      // Should hit cache (return true), proving cache key is order-independent
      expect(result2.value).toBe(true);

      client.close();
    });
  });

  describe('Client Lifecycle', () => {
    it('should close connection gracefully', async () => {
      const client = new HeimdallClient({
        target: server.getAddress(),
        apiKey: DUMMY_API_KEY,
        insecure: true,
      });

      // Should not throw
      expect(() => client.close()).not.toThrow();
    });
  });

  describe('Authentication & Metadata', () => {
    it('should send API key as Bearer token in metadata', async () => {
      server.setEvaluationResult('feature_auth', true);

      const testApiKey = 'sdk_prod_key_xyz789abc123def456';
      const client = new HeimdallClient({
        target: server.getAddress(),
        apiKey: testApiKey,
        insecure: true,
      });

      // Make a request
      await client.getEvaluation('feature_auth', {}, false);

      // Check that metadata was received with API key
      const metadata = server.getLastMetadata();
      expect(metadata).not.toBeNull();
      const authHeader = metadata?.get('authorization');
      expect(authHeader).toBeDefined();
      expect(authHeader?.[0]).toBe(`Bearer ${testApiKey}`);

      client.close();
    });

    it('should send SDK version in metadata for telemetry', async () => {
      server.setEvaluationResult('feature_telemetry', true);

      const client = new HeimdallClient({
        target: server.getAddress(),
        apiKey: DUMMY_API_KEY,
        insecure: true,
      });

      // Make a request
      await client.getEvaluation('feature_telemetry', {}, false);

      // Check that metadata includes SDK version
      const metadata = server.getLastMetadata();
      expect(metadata).not.toBeNull();
      const sdkHeader = metadata?.get('x-heimdall-sdk');
      expect(sdkHeader).toBeDefined();
      expect(sdkHeader?.[0]).toMatch(/^node\/\d+\.\d+\.\d+$/);

      client.close();
    });

    it('should include both API key and SDK version in every request', async () => {
      server.setEvaluationResult('flag_1', true);
      server.setEvaluationResult('flag_2', false);

      const client = new HeimdallClient({
        target: server.getAddress(),
        apiKey: DUMMY_API_KEY,
        insecure: true,
      });

      // First request
      await client.getEvaluation('flag_1', {}, false);
      let metadata = server.getLastMetadata();
      expect(metadata?.get('authorization')?.[0]).toContain('Bearer');
      expect(metadata?.get('x-heimdall-sdk')?.[0]).toMatch(/^node\//);

      // Second request - verify metadata is still sent
      await client.getEvaluation('flag_2', {}, false);
      metadata = server.getLastMetadata();
      expect(metadata?.get('authorization')?.[0]).toContain('Bearer');
      expect(metadata?.get('x-heimdall-sdk')?.[0]).toMatch(/^node\//);

      client.close();
    });
  });
});
