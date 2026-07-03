import { test, expect } from '@grafana/plugin-e2e';

test('smoke: should render query editor', async ({ panelEditPage, readProvisionedDataSource }) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await panelEditPage.datasource.set(ds.name);
  const row = panelEditPage.getQueryEditorRow('A');
  await expect(row.getByRole('textbox', { name: 'Dataset ID' })).toBeVisible();
  await expect(row.getByRole('textbox', { name: 'Variables' })).toBeVisible();
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
  await row.getByRole('textbox', { name: 'Dataset ID' }).fill('testDataset');
  await row.getByRole('textbox', { name: 'Dataset ID' }).blur();

  const queryReq = panelEditPage.waitForQueryDataRequest();
  await row.getByRole('textbox', { name: 'Variables' }).fill('temperature');
  await row.getByRole('textbox', { name: 'Variables' }).blur();

  await expect(await queryReq).toBeTruthy();
});
