// force timezone to UTC to allow tests to work regardless of local timezone
// generally used by snapshots, but can affect specific tests
process.env.TZ = 'UTC';

module.exports = {
  // Jest configuration provided by Grafana scaffolding
  ...require('./.config/jest.config'),
  coverageDirectory: 'coverage',
  coverageReporters: ['lcov', 'text'],
  reporters: process.env.CI
    ? ['default', ['jest-junit', { outputDirectory: 'coverage', outputName: 'frontend-junit.xml' }]]
    : ['default'],
};
