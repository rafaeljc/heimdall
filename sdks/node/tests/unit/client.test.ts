import * as grpc from '@grpc/grpc-js';
import { HeimdallClient } from '../../src/client';
import { createMockLogger } from './fixtures/mocks';

const DUMMY_API_KEY = 'test_key_dummy_1234567890abcdef';

describe('HeimdallClient', () => {
  describe('Configuration Validation', () => {
    it('should throw on empty target', () => {
      const mockLogger = createMockLogger();
      expect(
        () =>
          new HeimdallClient({
            target: '',
            apiKey: DUMMY_API_KEY,
            logger: mockLogger,
          }),
      ).toThrow('[Heimdall SDK] Configuration Error: "target" is required and cannot be empty.');
    });

    it('should throw on whitespace-only target', () => {
      const mockLogger = createMockLogger();
      expect(
        () =>
          new HeimdallClient({
            target: '   ',
            apiKey: DUMMY_API_KEY,
            logger: mockLogger,
          }),
      ).toThrow('[Heimdall SDK] Configuration Error: "target" is required and cannot be empty.');
    });

    it('should accept valid target', () => {
      const mockLogger = createMockLogger();
      expect(
        () =>
          new HeimdallClient({
            target: 'localhost:50051',
            apiKey: DUMMY_API_KEY,
            logger: mockLogger,
          }),
      ).not.toThrow();
    });

    it('should throw on empty API key', () => {
      const mockLogger = createMockLogger();
      expect(
        () =>
          new HeimdallClient({
            target: 'localhost:50051',
            apiKey: '',
            logger: mockLogger,
          }),
      ).toThrow('[Heimdall SDK] Configuration Error: "apiKey" is required and cannot be empty.');
    });

    it('should throw on whitespace-only API key', () => {
      const mockLogger = createMockLogger();
      expect(
        () =>
          new HeimdallClient({
            target: 'localhost:50051',
            apiKey: '   ',
            logger: mockLogger,
          }),
      ).toThrow('[Heimdall SDK] Configuration Error: "apiKey" is required and cannot be empty.');
    });

    it('should accept valid API key', () => {
      const mockLogger = createMockLogger();
      expect(
        () =>
          new HeimdallClient({
            target: 'localhost:50051',
            apiKey: DUMMY_API_KEY,
            logger: mockLogger,
          }),
      ).not.toThrow();
    });
  });

  describe('Timeout Configuration', () => {
    const validTarget = 'localhost:50051';

    it('should accept timeout within bounds', () => {
      const mockLogger = createMockLogger();
      expect(
        () =>
          new HeimdallClient({
            target: validTarget,
            apiKey: DUMMY_API_KEY,
            timeout: 5000,
            logger: mockLogger,
          }),
      ).not.toThrow();
    });

    it('should clamp timeout below minimum (20ms)', () => {
      const mockLogger = createMockLogger();
      const client = new HeimdallClient({
        target: validTarget,
        apiKey: DUMMY_API_KEY,
        timeout: 19,
        logger: mockLogger,
      });

      const warnLogs = mockLogger.logs.warn;
      expect(warnLogs.some((log: string) => log.includes('below minimum'))).toBe(true);
      client.close();
    });

    it('should clamp timeout above maximum (10000ms)', () => {
      const mockLogger = createMockLogger();
      const client = new HeimdallClient({
        target: validTarget,
        apiKey: DUMMY_API_KEY,
        timeout: 10001,
        logger: mockLogger,
      });

      const warnLogs = mockLogger.logs.warn;
      expect(warnLogs.some((log: string) => log.includes('exceeds maximum'))).toBe(true);
      client.close();
    });

    it('should handle NaN timeout', () => {
      const mockLogger = createMockLogger();
      const client = new HeimdallClient({
        target: validTarget,
        apiKey: DUMMY_API_KEY,
        timeout: NaN,
        logger: mockLogger,
      });

      const warnLogs = mockLogger.logs.warn;
      expect(warnLogs.some((log: string) => log.includes('Invalid timeout'))).toBe(true);
      client.close();
    });

    it('should use default timeout (5000ms) when not provided', () => {
      const mockLogger = createMockLogger();
      const client = new HeimdallClient({
        target: validTarget,
        apiKey: DUMMY_API_KEY,
        logger: mockLogger,
      });

      // If no warning is logged, default was used
      const warnLogs = mockLogger.logs.warn;
      expect(warnLogs.length).toBe(0);
      client.close();
    });
  });

  describe('Cache Configuration', () => {
    const validTarget = 'localhost:50051';

    it('should enable cache by default', () => {
      const mockLogger = createMockLogger();
      const client = new HeimdallClient({
        target: validTarget,
        apiKey: DUMMY_API_KEY,
        logger: mockLogger,
      });

      const debugLogs = mockLogger.logs.debug;
      expect(debugLogs.some((log: string) => log.includes('Cache enabled'))).toBe(true);
      client.close();
    });

    it('should disable cache when cacheTTL is 0', () => {
      const mockLogger = createMockLogger();
      const client = new HeimdallClient({
        target: validTarget,
        apiKey: DUMMY_API_KEY,
        cacheTTL: 0,
        logger: mockLogger,
      });

      const debugLogs = mockLogger.logs.debug;
      expect(debugLogs.some((log: string) => log.includes('Cache disabled'))).toBe(true);
      client.close();
    });

    it('should accept custom cache TTL and size', () => {
      const mockLogger = createMockLogger();
      const client = new HeimdallClient({
        target: validTarget,
        apiKey: DUMMY_API_KEY,
        cacheTTL: 30000,
        cacheSize: 500,
        logger: mockLogger,
      });

      const debugLogs = mockLogger.logs.debug;
      expect(
        debugLogs.some(
          (log: string) =>
            log.includes('Cache enabled') && log.includes('max=500') && log.includes('ttl=30000'),
        ),
      ).toBe(true);
      client.close();
    });
  });

  describe('Logger Integration', () => {
    const validTarget = 'localhost:50051';

    it('should use injected logger', () => {
      const mockLogger = createMockLogger();
      new HeimdallClient({
        target: validTarget,
        apiKey: DUMMY_API_KEY,
        logger: mockLogger,
      });

      expect(mockLogger.logs.info.length).toBeGreaterThan(0);
      expect(mockLogger.logs.info[0]).toMatch(/Connected to Heimdall at/);
    });

    it('should use default logger when not provided', () => {
      const consoleSpy = jest.spyOn(console, 'log').mockImplementation();
      try {
        new HeimdallClient({
          target: validTarget,
          apiKey: DUMMY_API_KEY,
        });

        expect(consoleSpy).toHaveBeenCalledWith(
          expect.stringMatching(/\[Heimdall SDK v\d+\.\d+\.\d+\] Connected to Heimdall at/),
        );
      } finally {
        consoleSpy.mockRestore();
      }
    });
  });

  describe('gRPC Connection', () => {
    it('should use insecure credentials when specified', () => {
      const mockLogger = createMockLogger();
      const credentialsSpy = jest.spyOn(grpc.credentials, 'createInsecure');

      try {
        new HeimdallClient({
          target: 'localhost:50051',
          apiKey: DUMMY_API_KEY,
          insecure: true,
          logger: mockLogger,
        });

        expect(credentialsSpy).toHaveBeenCalled();
      } finally {
        credentialsSpy.mockRestore();
      }
    });

    it('should use SSL credentials by default', () => {
      const mockLogger = createMockLogger();
      const credentialsSpy = jest.spyOn(grpc.credentials, 'createSsl');

      try {
        new HeimdallClient({
          target: 'localhost:50051',
          apiKey: DUMMY_API_KEY,
          logger: mockLogger,
        });

        expect(credentialsSpy).toHaveBeenCalled();
      } finally {
        credentialsSpy.mockRestore();
      }
    });
  });

  describe('Close Method', () => {
    it('should close the gRPC client', () => {
      const mockLogger = createMockLogger();
      const client = new HeimdallClient({
        target: 'localhost:50051',
        apiKey: DUMMY_API_KEY,
        logger: mockLogger,
      });

      // jest.fn() on client.close would require mocking the entire client
      // This is a smoke test to ensure close doesn't throw
      expect(() => client.close()).not.toThrow();
    });
  });
});
