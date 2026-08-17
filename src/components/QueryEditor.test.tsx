import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryEditorProps } from '@grafana/data';

import { QueryEditor } from './QueryEditor';
import { DataSource } from '../datasource';
import { ErddapDataSourceOptions, ErddapQuery, ErddapVariable } from '../types';

type Props = QueryEditorProps<DataSource, ErddapQuery, ErddapDataSourceOptions>;

const VARIABLES: ErddapVariable[] = [
  { name: 'time', type: 'double', units: 'seconds since 1970-01-01' },
  { name: 'temperature', type: 'float', units: 'celsius', longName: 'Mean Temperature' },
  { name: 'salinity', type: 'float' },
];

function setup(query: Partial<ErddapQuery> = {}, overrides: Partial<jest.Mocked<DataSource>> = {}) {
  const searchDatasets = jest.fn().mockResolvedValue({
    datasets: [
      { id: 'M01_sbe37_all', title: 'Buoy M01', institution: 'NERACOOS', tabledapSupported: true },
      { id: 'grid_only', title: 'Grid only', institution: 'NOAA', tabledapSupported: false },
    ],
    truncated: false,
  });
  const getVariables = jest.fn().mockResolvedValue({ variables: VARIABLES });

  const datasource = { searchDatasets, getVariables, ...overrides } as unknown as DataSource;
  const onChange = jest.fn();
  const onRunQuery = jest.fn();

  const props = {
    datasource,
    query: { refId: 'A', ...query },
    onChange,
    onRunQuery,
  } as unknown as Props;

  const view = render(<QueryEditor {...props} />);

  return { ...view, datasource, searchDatasets, getVariables, onChange, onRunQuery, props };
}

/** MultiCombobox and Combobox both render their input with role combobox. */
function comboboxFor(name: RegExp) {
  return screen.getByRole('combobox', { name });
}

describe('QueryEditor', () => {
  it('does not fetch variables while no dataset is selected', () => {
    const { getVariables } = setup();

    expect(getVariables).not.toHaveBeenCalled();
    // MultiCombobox styles itself as disabled but does not set the DOM
    // attribute, so the placeholder is what actually tells the user why.
    expect(comboboxFor(/Variables/)).toHaveAttribute('placeholder', 'Select a dataset first');
  });

  it('fetches variables exactly once for the selected dataset', async () => {
    const { getVariables } = setup({ datasetId: 'M01_sbe37_all' });

    await waitFor(() => expect(getVariables).toHaveBeenCalledTimes(1));
    expect(getVariables).toHaveBeenCalledWith('M01_sbe37_all');
  });

  it('refetches variables when the dataset changes', async () => {
    const { getVariables, rerender, props } = setup({ datasetId: 'first' });

    await waitFor(() => expect(getVariables).toHaveBeenCalledWith('first'));

    rerender(<QueryEditor {...props} query={{ ...props.query, datasetId: 'second' }} />);

    await waitFor(() => expect(getVariables).toHaveBeenCalledWith('second'));
    expect(getVariables).toHaveBeenCalledTimes(2);
  });

  it('never offers the implicit time variable', async () => {
    setup({ datasetId: 'M01_sbe37_all' });

    fireEvent.click(comboboxFor(/Variables/));

    expect(await screen.findByRole('option', { name: /temperature/ })).toBeInTheDocument();
    expect(screen.queryByRole('option', { name: /^time$/ })).not.toBeInTheDocument();
  });

  it('describes variables with their long name and units', async () => {
    setup({ datasetId: 'M01_sbe37_all' });

    fireEvent.click(comboboxFor(/Variables/));

    expect(await screen.findByText('Mean Temperature (celsius)')).toBeInTheDocument();
  });

  it('shows variables already stored on the query as selected', async () => {
    const { onChange } = setup({ datasetId: 'M01_sbe37_all', variables: 'temperature, salinity' });

    // jsdom reports a zero-width container, so MultiCombobox collapses all but
    // the first pill behind an overflow counter.
    await waitFor(() => expect(screen.getByLabelText('Remove temperature')).toBeInTheDocument());

    // Removing one proves the other was parsed out of the comma-joined string.
    fireEvent.click(screen.getByLabelText('Remove temperature'));

    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ variables: 'salinity' }));
  });

  it('joins selected variables back into a comma separated string', async () => {
    const { onChange, onRunQuery } = setup({ datasetId: 'M01_sbe37_all', variables: 'temperature' });

    fireEvent.click(comboboxFor(/Variables/));
    fireEvent.click(await screen.findByRole('option', { name: /salinity/ }));

    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ variables: 'temperature,salinity' }));
    expect(onRunQuery).toHaveBeenCalled();
  });

  it('surfaces an error but stays interactive when variables cannot be loaded', async () => {
    const getVariables = jest.fn().mockRejectedValue(new Error('upstream boom'));
    setup({ datasetId: 'M01_sbe37_all' }, { getVariables });

    expect(await screen.findByText('Could not load variables. You can still type variable names.')).toBeInTheDocument();
    expect(comboboxFor(/Variables/)).toBeEnabled();
  });

  it('offers datasets from the search endpoint and marks griddap-only ones', async () => {
    const { searchDatasets } = setup();

    fireEvent.click(comboboxFor(/Dataset ID/));

    expect(await screen.findByRole('option', { name: /Buoy M01/ })).toBeInTheDocument();
    expect(screen.getByText(/not supported — tabledap only/)).toBeInTheDocument();
    expect(searchDatasets).toHaveBeenCalled();
  });

  it('clears previously selected variables when the dataset changes', async () => {
    const { onChange, onRunQuery } = setup({ datasetId: 'old_dataset', variables: 'temperature' });

    fireEvent.click(comboboxFor(/Dataset ID/));
    fireEvent.click(await screen.findByRole('option', { name: /Buoy M01/ }));

    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ datasetId: 'M01_sbe37_all', variables: '' }));
    expect(onRunQuery).toHaveBeenCalled();
  });

  it('keeps the constraints field as a plain text input', () => {
    setup({ constraints: 'station="A01"' });

    expect(screen.getByRole('textbox', { name: /Constraints/ })).toHaveValue('station="A01"');
  });
});
