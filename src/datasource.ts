import { CoreApp, DataSourceInstanceSettings } from '@grafana/data';
import { DataSourceWithBackend } from '@grafana/runtime';

import { DEFAULT_QUERY, ErddapDataSourceOptions, ErddapQuery } from './types';

export class DataSource extends DataSourceWithBackend<ErddapQuery, ErddapDataSourceOptions> {
  constructor(instanceSettings: DataSourceInstanceSettings<ErddapDataSourceOptions>) {
    super(instanceSettings);
  }

  getDefaultQuery(_: CoreApp): Partial<ErddapQuery> {
    return DEFAULT_QUERY;
  }

  filterQuery(query: ErddapQuery): boolean {
    // if the dataset or variables have not been provided, prevent the query from being executed
    return !!query.datasetId?.trim() && !!query.variables?.trim();
  }
}
