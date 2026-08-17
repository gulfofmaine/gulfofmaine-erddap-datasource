import {
  CoreApp,
  DataSourceInstanceSettings,
  LegacyMetricFindQueryOptions,
  MetricFindValue,
  ScopedVars,
} from '@grafana/data';
import { DataSourceWithBackend, getTemplateSrv } from '@grafana/runtime';

import { formatErddapValue } from './interpolation';
import {
  DEFAULT_QUERY,
  DatasetSearchResponse,
  ErddapDataSourceOptions,
  ErddapQuery,
  ErddapVariableQuery,
  VariablesResponse,
} from './types';
import { ErddapVariableSupport } from './variableSupport';

/** The shape of the `/distinct` CallResource endpoint's response body. */
interface DistinctResponse {
  values: string[];
}

export class DataSource extends DataSourceWithBackend<ErddapQuery, ErddapDataSourceOptions> {
  constructor(instanceSettings: DataSourceInstanceSettings<ErddapDataSourceOptions>) {
    super(instanceSettings);
    this.variables = new ErddapVariableSupport(this);
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

  /**
   * Resolves a dashboard query variable to the distinct values one dataset
   * variable takes.
   *
   * Accepts either the typed form produced by {@link VariableQueryEditor} or a
   * `"datasetId.variable"` string, which is what a hand-authored dashboard JSON
   * is most likely to carry. Every field is interpolated first, so one variable
   * can be defined in terms of another.
   *
   * An incomplete query resolves to no values rather than raising: a
   * half-typed variable definition should show no options, not an error.
   *
   * @param query the variable query, typed or in string form
   * @param options carries the scoped variables to interpolate with
   * @returns the distinct values, in the order ERDDAP returned them
   */
  async metricFindQuery(
    query: ErddapVariableQuery | string,
    options?: LegacyMetricFindQueryOptions
  ): Promise<MetricFindValue[]> {
    const templateSrv = getTemplateSrv();
    const { scopedVars } = options ?? {};
    // A scalar lookup value, so the constraint formatter does not apply here.
    const resolve = (field?: string) => templateSrv.replace(field, scopedVars).trim();

    let raw: Pick<ErddapVariableQuery, 'datasetId' | 'variable' | 'constraints'>;

    if (typeof query === 'string') {
      // Dataset IDs and variable names are both [A-Za-z0-9_]+, but splitting on
      // the first separator only keeps a stray dot inside the name intact
      // rather than silently truncating it.
      const separator = query.indexOf('.');
      if (separator === -1) {
        return [];
      }
      raw = { datasetId: query.slice(0, separator), variable: query.slice(separator + 1) };
    } else {
      raw = query;
    }

    const datasetId = resolve(raw.datasetId);
    const variable = resolve(raw.variable);
    const constraints = resolve(raw.constraints);

    if (!datasetId || !variable) {
      return [];
    }

    const response = await this.getResource<DistinctResponse>('distinct', {
      datasetId,
      variable,
      // The backend distinguishes "no constraints" from an empty expression.
      ...(constraints ? { constraints } : {}),
    });

    // Already sorted and deduplicated by ERDDAP's distinct(); re-sorting would
    // only diverge from what the server considers canonical order.
    return (response?.values ?? []).map((value) => ({ text: value }));
  }
}
