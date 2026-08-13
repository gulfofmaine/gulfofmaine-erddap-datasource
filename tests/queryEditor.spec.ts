import { test, expect } from '@grafana/plugin-e2e';

test('smoke: should render query editor', async ({ panelEditPage, readProvisionedDataSource }) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await panelEditPage.datasource.set(ds.name);
  const row = panelEditPage.getQueryEditorRow('A');
  await expect(row.getByRole('combobox', { name: 'Dataset ID' })).toBeVisible();
  await expect(row.getByRole('combobox', { name: 'Variables' })).toBeVisible();
  await expect(row.getByRole('textbox', { name: 'Constraints' })).toBeVisible();
});

test('should trigger new query when Dataset ID and Variables are set', async ({
  panelEditPage,
  readProvisionedDataSource,
}) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await panelEditPage.datasource.set(ds.name);
  const row = panelEditPage.getQueryEditorRow('A');

  // filterQuery requires both datasetId and variables to be non-blank before a
  // request is issued, so filling Dataset ID alone must not trigger a request.
  // Both fields are now Comboboxes: typing a value only commits it once an
  // option is accepted (Enter selects the "Use this value" custom-value row),
  // not on fill()/blur() alone.
  const datasetCombobox = row.getByRole('combobox', { name: 'Dataset ID' });
  await datasetCombobox.fill('testDataset');
  await datasetCombobox.press('Enter');

  const queryReq = panelEditPage.waitForQueryDataRequest();
  const variablesCombobox = row.getByRole('combobox', { name: 'Variables' });
  await variablesCombobox.fill('temperature');
  await variablesCombobox.press('Enter');
  await variablesCombobox.blur();

  await expect(await queryReq).toBeTruthy();
});
