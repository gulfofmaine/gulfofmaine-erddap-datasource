import { DataSourceJsonData } from '@grafana/data';
import { DataQuery } from '@grafana/schema';

export interface ErddapQuery extends DataQuery {
  datasetId?: string;
  variables?: string;
  constraints?: string;
}

export const DEFAULT_QUERY: Partial<ErddapQuery> = {};

/**
 * These are options configured for each DataSource instance
 */
export interface ErddapDataSourceOptions extends DataSourceJsonData {
  baseUrl?: string;
}
