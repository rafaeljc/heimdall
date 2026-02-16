module.exports = {
  moduleFileExtensions: ['js', 'json', 'ts'],
  rootDir: '.',
  testRegex: '.*\\.integration\\.ts$',
  transform: {
    '^.+\\.(t|j)s$': ['ts-jest', { tsconfig: 'tsconfig.test.json' }],
  },
  testEnvironment: 'node',
  testTimeout: 30000, // Longer timeout for integration tests
  collectCoverageFrom: ['**/*.(t|j)s'],
  coverageDirectory: './coverage/integration',
  coveragePathIgnorePatterns: ['/node_modules/'],
};
