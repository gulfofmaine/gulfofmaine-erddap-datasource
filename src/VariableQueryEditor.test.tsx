import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { QueryEditorProps } from '@grafana/data';

import { VariableQueryEditor } from './VariableQueryEditor';
import { DataSource } from './datasource';
import { ErddapDataSourceOptions, ErddapQuery, ErddapVariableQuery } from './types';

type Props = QueryEditorProps<DataSource, ErddapQuery, ErddapDataSourceOptions, ErddapVariableQuery>;

function setup(query: Partial<ErddapVariableQuery> = {}) {
  const onChange = jest.fn();
  const onRunQuery = jest.fn();

  const props = {
    datasource: {} as DataSource,
    query: { refId: 'A', ...query },
    onChange,
    onRunQuery,
  } as unknown as Props;

  render(<VariableQueryEditor {...props} />);

  return { onChange, onRunQuery };
}

describe('VariableQueryEditor', () => {
  it('renders the saved query', () => {
    setup({ datasetId: 'M01', variable: 'station', constraints: 'depth<2' });

    expect(screen.getByLabelText('Dataset ID')).toHaveValue('M01');
    expect(screen.getByLabelText('Variable')).toHaveValue('station');
    expect(screen.getByLabelText('Constraints')).toHaveValue('depth<2');
  });

  it.each`
    label            | field
    ${'Dataset ID'}  | ${'datasetId'}
    ${'Variable'}    | ${'variable'}
    ${'Constraints'} | ${'constraints'}
  `('updates $field when $label changes', ({ label, field }) => {
    const { onChange, onRunQuery } = setup();

    fireEvent.change(screen.getByLabelText(label), { target: { value: 'typed' } });

    expect(onChange).toHaveBeenCalledWith({ refId: 'A', [field]: 'typed' });
    // Editing alone should not re-run; the query runs once the field is left.
    expect(onRunQuery).not.toHaveBeenCalled();

    fireEvent.blur(screen.getByLabelText(label));
    expect(onRunQuery).toHaveBeenCalled();
  });

  it('renders empty fields rather than "undefined" for a new variable', () => {
    setup();

    expect(screen.getByLabelText('Dataset ID')).toHaveValue('');
    expect(screen.getByLabelText('Variable')).toHaveValue('');
    expect(screen.getByLabelText('Constraints')).toHaveValue('');
  });
});
