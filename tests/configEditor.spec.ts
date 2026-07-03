import { test, expect } from '@grafana/plugin-e2e';
import { ErddapDataSourceOptions } from '../src/types';

test('smoke: should render config editor', async ({ createDataSourceConfigPage, readProvisionedDataSource, page }) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await createDataSourceConfigPage({ type: ds.type });
  await expect(page.getByRole('textbox', { name: 'ERDDAP URL' })).toBeVisible();
});

// NOTE: this test requires the Grafana container to have network access to the
// public ERDDAP server referenced by the provisioned datasource (see
// provisioning/datasources/datasources.yml). It will fail in offline/sandboxed
// environments where that server is unreachable.
test('"Save & test" should be successful when configuration is valid', async ({
  createDataSourceConfigPage,
  readProvisionedDataSource,
  page,
}) => {
  const ds = await readProvisionedDataSource<ErddapDataSourceOptions>({ fileName: 'datasources.yml' });
  const configPage = await createDataSourceConfigPage({ type: ds.type });
  await page.getByRole('textbox', { name: 'ERDDAP URL' }).fill(ds.jsonData.baseUrl ?? '');
  await expect(configPage.saveAndTest()).toBeOK();
});

test('"Save & test" should fail when configuration is invalid', async ({
  createDataSourceConfigPage,
  readProvisionedDataSource,
  page,
}) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  const configPage = await createDataSourceConfigPage({ type: ds.type });
  await page.getByRole('textbox', { name: 'ERDDAP URL' }).fill('');
  await expect(configPage.saveAndTest()).not.toBeOK();
  await expect(configPage).toHaveAlert('error', { hasText: 'ERDDAP base URL is missing' });
});
