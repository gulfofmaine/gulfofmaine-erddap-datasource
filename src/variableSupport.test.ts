import { DataQueryRequest, VariableSupportType } from '@grafana/data';
import { lastValueFrom } from 'rxjs';

import { VariableQueryEditor } from './VariableQueryEditor';
import type { DataSource } from './datasource';
import { ErddapVariableQuery } from './types';
import { ErddapVariableSupport } from './variableSupport';

function setup(values = [{ text: 'A01' }, { text: 'B01' }]) {
  const metricFindQuery = jest.fn().mockResolvedValue(values);
  const datasource = { metricFindQuery } as unknown as DataSource;
  return { support: new ErddapVariableSupport(datasource), metricFindQuery };
}

describe('ErddapVariableSupport', () => {
  it('registers as a custom variable support with the plugin editor', () => {
    const { support } = setup();

    expect(support.getType()).toBe(VariableSupportType.Custom);
    expect(support.editor).toBe(VariableQueryEditor);
  });

  it('resolves the first target through metricFindQuery, forwarding scope and range', async () => {
    const { support, metricFindQuery } = setup();
    const target: ErddapVariableQuery = { refId: 'A', datasetId: 'M01', variable: 'station' };
    const scopedVars = { dataset: { text: 'M01', value: 'M01' } };
    const range = { from: 'from', to: 'to' };

    const response = await lastValueFrom(
      support.query({ targets: [target], scopedVars, range } as unknown as DataQueryRequest<ErddapVariableQuery>)
    );

    expect(metricFindQuery).toHaveBeenCalledWith(target, { scopedVars, range });
    expect(response).toEqual({ data: [{ text: 'A01' }, { text: 'B01' }] });
  });
});
