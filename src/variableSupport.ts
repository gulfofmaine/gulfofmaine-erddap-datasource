import { CustomVariableSupport, DataQueryRequest, DataQueryResponse } from '@grafana/data';
import { Observable, from } from 'rxjs';
import { map } from 'rxjs/operators';

import { VariableQueryEditor } from './VariableQueryEditor';
import type { DataSource } from './datasource';
import { ErddapVariableQuery } from './types';

/**
 * Wires the ERDDAP query variable editor into Grafana's variable machinery.
 *
 * The work itself lives in `DataSource.metricFindQuery`, so the string form of
 * a variable query (used by hand-authored dashboard JSON) and the typed form
 * from the editor resolve through exactly the same path.
 */
export class ErddapVariableSupport extends CustomVariableSupport<DataSource, ErddapVariableQuery> {
  editor = VariableQueryEditor;

  constructor(private readonly datasource: DataSource) {
    super();
  }

  query(request: DataQueryRequest<ErddapVariableQuery>): Observable<DataQueryResponse> {
    return from(
      this.datasource.metricFindQuery(request.targets[0], { scopedVars: request.scopedVars, range: request.range })
    ).pipe(map((data) => ({ data })));
  }
}
