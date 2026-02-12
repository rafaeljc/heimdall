module.exports = {
  parser: '@typescript-eslint/parser',
  parserOptions: {
    project: 'tsconfig.json',
    tsconfigRootDir: __dirname,
    sourceType: 'module',
  },
  plugins: ['@typescript-eslint/eslint-plugin'],
  extends: [
    'plugin:@typescript-eslint/recommended', // Uses the recommended rules from the @typescript-eslint/eslint-plugin
    'plugin:prettier/recommended',           // Enables eslint-plugin-prettier and eslint-config-prettier. MUST be last.
  ],
  root: true,
  env: {
    node: true,
    jest: true,
  },
  // Ignore build artifacts and generated protobuf code
  ignorePatterns: ['.eslintrc.js', 'dist', 'coverage', 'src/generated/**/*'],
  rules: {
    // Explicitly warn usage of 'any' type to encourage strict typing
    '@typescript-eslint/no-explicit-any': 'warn',
    
    // Disable rules that conflict with some gRPC patterns or preferences
    '@typescript-eslint/interface-name-prefix': 'off',
    '@typescript-eslint/explicit-function-return-type': 'off',
    '@typescript-eslint/explicit-module-boundary-types': 'off',
    
    // Enforce Prettier formatting as an ESLint error
    'prettier/prettier': ['error', { endOfLine: 'auto' }],
    
    // Ensure console.log is not left in production code (use a logger instead)
    'no-console': ['warn', { allow: ['warn', 'error'] }],
  },
};
