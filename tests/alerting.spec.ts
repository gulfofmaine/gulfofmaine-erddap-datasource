import { test, expect } from '@grafana/plugin-e2e';

// `evaluate()` clicks Preview, which calls Grafana's /api/v1/eval endpoint.
// That runs the query through the backend and into the expression engine,
// which rejects anything that is not a wide time series with "input data must
// be a wide series". A passing evaluation is therefore the end-to-end proof
// that this datasource is alert-compatible.
//
// Unlike the other specs, this one issues a real request to the provisioned
// ERDDAP server (https://data.neracoos.org/erddap).
test('should evaluate a provisioned alert rule', async ({ gotoAlertRuleEditPage, readProvisionedAlertRule }) => {
  const alertRule = await readProvisionedAlertRule({ fileName: 'alerts.yml' });
  const alertRuleEditPage = await gotoAlertRuleEditPage(alertRule);

  await expect(alertRuleEditPage.evaluate()).toBeOK();
});
