import { DataSourceJsonData } from '@grafana/data';
import { DataQuery } from '@grafana/schema';

export interface ErddapQuery extends DataQuery {
  datasetId?: string;
  variables?: string;
  constraints?: string;
}

export const DEFAULT_QUERY: Partial<ErddapQuery> = {};

/**
 * A dashboard *query variable*: the distinct values one dataset variable takes.
 *
 * Separate from {@link ErddapQuery} because a variable query returns a list of
 * values rather than time series, so it names a single variable and has no
 * concept of the requested field list.
 */
export interface ErddapVariableQuery extends DataQuery {
  datasetId?: string;
  variable?: string;
  constraints?: string;
}

/**
 * These are options configured for each DataSource instance
 */
export interface ErddapDataSourceOptions extends DataSourceJsonData {
  baseUrl?: string;
}

/**
 * A dataset as returned by the `/datasets` discovery endpoint.
 */
export interface ErddapDatasetSummary {
  id: string;
  title?: string;
  institution?: string;
  summary?: string;
  tabledapSupported: boolean;
}

/**
 * A variable as returned by the `/variables` discovery endpoint.
 */
export interface ErddapVariable {
  name: string;
  type: string;
  units?: string;
  longName?: string;
}

export interface DatasetSearchResponse {
  datasets: ErddapDatasetSummary[];
  truncated: boolean;
}

export interface VariablesResponse {
  variables: ErddapVariable[];
}
