module.exports = {
  projects: [
    {
      displayName: 'unit',
      testMatch: ['<rootDir>/tests/unit/**/*.test.ts'],
      moduleFileExtensions: ['js', 'json', 'ts'],
      rootDir: '.',
      transform: {
        '^.+\\.(t|j)s$': 'ts-jest',
      },
      testEnvironment: 'node',
      collectCoverageFrom: ['src/**/*.(t|j)s'],
      coverageDirectory: './coverage/unit',
      coveragePathIgnorePatterns: ['/node_modules/', '/generated/', '.d.ts'],
      roots: ['<rootDir>/src', '<rootDir>/tests/unit'],
      moduleNameMapper: {
        '^@/(.*)$': '<rootDir>/src/$1',
      },
    },
    {
      displayName: 'integration',
      testMatch: ['<rootDir>/tests/integration/**/*.integration.ts'],
      moduleFileExtensions: ['js', 'json', 'ts'],
      rootDir: '.',
      transform: {
        '^.+\\.(t|j)s$': 'ts-jest',
      },
      testEnvironment: 'node',
      testTimeout: 30000,
      collectCoverageFrom: ['src/**/*.(t|j)s'],
      coverageDirectory: './coverage/integration',
      coveragePathIgnorePatterns: ['/node_modules/', '/generated/', '.d.ts'],
      roots: ['<rootDir>/src', '<rootDir>/tests/integration'],
      moduleNameMapper: {
        '^@/(.*)$': '<rootDir>/src/$1',
      },
    },
  ],
};
