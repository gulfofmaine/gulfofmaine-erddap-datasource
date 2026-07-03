import { DataSourceInstanceSettings } from '@grafana/data';

import { DataSource } from './datasource';
import { ErddapDataSourceOptions, ErddapQuery } from './types';

function createDataSource() {
  const instanceSettings = {} as DataSourceInstanceSettings<ErddapDataSourceOptions>;
  return new DataSource(instanceSettings);
}

function query(datasetId?: string, variables?: string): ErddapQuery {
  return { refId: 'A', datasetId, variables };
}

describe('filterQuery', () => {
  const ds = createDataSource();

  it.each`
    description                                | datasetId    | variables        | expected
    ${'undefined datasetId, undefined vars'}   | ${undefined} | ${undefined}     | ${false}
    ${'empty datasetId, empty vars'}           | ${''}        | ${''}            | ${false}
    ${'whitespace-only datasetId and vars'}    | ${'   '}     | ${'   '}         | ${false}
    ${'valid datasetId, undefined vars'}       | ${'M01'}     | ${undefined}     | ${false}
    ${'undefined datasetId, valid vars'}       | ${undefined} | ${'temperature'} | ${false}
    ${'valid datasetId, empty vars'}           | ${'M01'}     | ${''}            | ${false}
    ${'valid datasetId, whitespace-only vars'} | ${'M01'}     | ${'   '}         | ${false}
    ${'whitespace-only datasetId, valid vars'} | ${'   '}     | ${'temperature'} | ${false}
    ${'valid datasetId, valid vars'}           | ${'M01'}     | ${'temperature'} | ${true}
  `('returns $expected when $description', ({ datasetId, variables, expected }) => {
    expect(ds.filterQuery(query(datasetId, variables))).toBe(expected);
  });
});
