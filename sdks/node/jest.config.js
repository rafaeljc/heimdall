module.exports = {
  moduleFileExtensions: ['js', 'json', 'ts'],
  rootDir: 'src',
  // Look for .test.ts files
  testRegex: '.*\\.test\\.ts$',
  transform: {
    '^.+\\.(t|j)s$': ['ts-jest', { tsconfig: 'tsconfig.test.json' }],
  },
  collectCoverageFrom: ['**/*.(t|j)s'],
  coverageDirectory: '../coverage',
  testEnvironment: 'node',
  // Do not track coverage for generated Protobuf code or node_modules
  coveragePathIgnorePatterns: ['/node_modules/', '/generated/', '.d.ts'],
  // Enforce high coverage thresholds for the SDK (Optional but recommended for Big Tech)
  /*
  coverageThreshold: {
    global: {
      branches: 80,
      functions: 80,
      lines: 80,
      statements: 80,
    },
  },
  */
};
