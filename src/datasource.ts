import { CoreApp, DataSourceInstanceSettings } from '@grafana/data';
import { DataSourceWithBackend } from '@grafana/runtime';

import { DEFAULT_QUERY, DatasetSearchResponse, ErddapDataSourceOptions, ErddapQuery, VariablesResponse } from './types';

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

  /**
   * Searches the configured ERDDAP server for datasets matching free text.
   *
   * @param q free-text search terms
   * @param limit maximum number of datasets to return
   * @returns matching dataset summaries; rejects when the server errors
   */
  async searchDatasets(q: string, limit = 100): Promise<DatasetSearchResponse> {
    return this.getResource('datasets', { q, limit });
  }

  /**
   * Lists the variables a dataset exposes, in declaration order.
   *
   * The response includes coordinate variables such as `time`; callers that
   * offer these for selection should filter `time` out because the backend
   * always requests it.
   *
   * @param datasetId the ERDDAP dataset ID
   * @returns the dataset's variables; rejects when the server errors
   */
  async getVariables(datasetId: string): Promise<VariablesResponse> {
    return this.getResource('variables', { datasetId });
  }
}
