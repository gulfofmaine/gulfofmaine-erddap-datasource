import { test, expect } from '@grafana/plugin-e2e';
import { Page } from '@playwright/test';

// MultiCombobox renders its options list as checkboxes in a portal detached
// from the query editor row, and (unlike the single-select Combobox used for
// Dataset ID) pressing Enter after typing does not commit the highlighted
// option — only clicking it does. The trailing description text on a custom
// value option ("Use custom value" vs. "Use this variable name") has also
// been observed to differ across the @grafana/ui versions bundled by
// different Grafana releases, so match on the typed value only.
async function selectCustomVariable(page: Page, value: string) {
  await page.getByRole('option', { name: new RegExp(value, 'i') }).click();
}

test('smoke: should render query editor', async ({ panelEditPage, readProvisionedDataSource }) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await panelEditPage.datasource.set(ds.name);
  const row = panelEditPage.getQueryEditorRow('A');
  await expect(row.getByRole('combobox', { name: 'Dataset ID' })).toBeVisible();
  // Located by data-testid rather than accessible name: older @grafana/ui
  // versions (bundled by Grafana releases still in the e2e matrix) compute
  // this MultiCombobox's accessible name from its placeholder rather than
  // its associated label when disabled, so "Variables" isn't always resolvable.
  await expect(row.getByTestId('query-editor-variables')).toBeVisible();
  await expect(row.getByRole('textbox', { name: 'Constraints' })).toBeVisible();
});

test('should trigger new query when Dataset ID and Variables are set', async ({
  panelEditPage,
  readProvisionedDataSource,
  page,
}) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await panelEditPage.datasource.set(ds.name);
  const row = panelEditPage.getQueryEditorRow('A');

  // filterQuery requires both datasetId and variables to be non-blank before a
  // request is issued, so filling Dataset ID alone must not trigger a request.
  // Dataset ID is a single-select Combobox: typing a value and pressing Enter
  // commits it as a custom value.
  const datasetCombobox = row.getByRole('combobox', { name: 'Dataset ID' });
  await datasetCombobox.fill('testDataset');
  await datasetCombobox.press('Enter');

  const queryReq = panelEditPage.waitForQueryDataRequest();
  const variablesCombobox = row.getByTestId('query-editor-variables');
  await variablesCombobox.fill('temperature');
  await selectCustomVariable(page, 'temperature');

  await expect(await queryReq).toBeTruthy();
});
