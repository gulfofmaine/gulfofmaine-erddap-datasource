import { CoreApp, DataSourceInstanceSettings, ScopedVars } from '@grafana/data';
import { DataSourceWithBackend, getTemplateSrv } from '@grafana/runtime';

import { formatErddapValue } from './interpolation';
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
   * Interpolates dashboard variables into a query before it is sent to the
   * backend.
   *
   * `datasetId` is deliberately left alone: a query targets exactly one
   * dataset, and panel repeat already supplies per-panel values through
   * `scopedVars`.
   *
   * @param query the query as configured in the editor
   * @param scopedVars panel- and row-scoped variable values
   * @returns the query with `constraints` and `variables` interpolated
   */
  applyTemplateVariables(query: ErddapQuery, scopedVars: ScopedVars): ErddapQuery {
    const templateSrv = getTemplateSrv();

    return {
      ...query,
      // Absent stays absent: the backend treats an empty constraint string as
      // "no constraints", and an empty variable list as an invalid query.
      constraints: query.constraints
        ? templateSrv.replace(query.constraints, scopedVars, formatErddapValue)
        : query.constraints,
      variables: query.variables ? templateSrv.replace(query.variables, scopedVars, 'csv') : query.variables,
    };
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
