import React, { ChangeEvent, useCallback, useEffect, useState } from 'react';
import { Combobox, ComboboxOption, InlineField, Input, MultiCombobox, Stack } from '@grafana/ui';
import { QueryEditorProps } from '@grafana/data';
import { DataSource } from '../datasource';
import { ErddapDatasetSummary, ErddapDataSourceOptions, ErddapQuery, ErddapVariable } from '../types';

type Props = QueryEditorProps<DataSource, ErddapQuery, ErddapDataSourceOptions>;

const VARIABLES_ERROR = 'Could not load variables. You can still type variable names.';

/** ERDDAP always returns time; the backend adds it to every query itself. */
const IMPLICIT_VARIABLE = 'time';

interface VariablesState {
  /** The dataset the options belong to, so stale results can be ignored. */
  datasetId: string;
  failed: boolean;
  options: Array<ComboboxOption<string>>;
}

function toDatasetOption(dataset: ErddapDatasetSummary): ComboboxOption<string> {
  const parts = [dataset.institution, dataset.tabledapSupported ? undefined : 'not supported — tabledap only'].filter(
    Boolean
  );

  return {
    value: dataset.id,
    label: dataset.title || dataset.id,
    description: parts.length ? parts.join(' — ') : undefined,
  };
}

function toVariableOption(variable: ErddapVariable): ComboboxOption<string> {
  const description = variable.units ? `${variable.longName ?? variable.name} (${variable.units})` : variable.longName;

  return {
    value: variable.name,
    label: variable.name,
    description,
  };
}

/** ERDDAP variable names cannot contain commas, so this round-trip is lossless. */
function splitVariables(variables?: string): string[] {
  return (variables ?? '')
    .split(',')
    .map((name) => name.trim())
    .filter(Boolean);
}

export function QueryEditor({ datasource, query, onChange, onRunQuery }: Props) {
  const { datasetId, variables, constraints } = query;

  const [datasetOption, setDatasetOption] = useState<ComboboxOption<string> | null>(null);
  // Kept as one record tagged with its dataset so that the loading and empty
  // states can be derived during render rather than written back from the
  // effect, which would trigger cascading renders.
  const [fetchedVariables, setFetchedVariables] = useState<VariablesState>({
    datasetId: '',
    failed: false,
    options: [],
  });

  useEffect(() => {
    if (!datasetId) {
      return;
    }

    // Guards against a slower earlier request overwriting a newer dataset's
    // variables when the user switches datasets mid-flight.
    let cancelled = false;

    datasource
      .getVariables(datasetId)
      .then((response) => {
        if (cancelled) {
          return;
        }
        setFetchedVariables({
          datasetId,
          failed: false,
          options: (response?.variables ?? [])
            .filter((variable) => variable.name !== IMPLICIT_VARIABLE)
            .map(toVariableOption),
        });
      })
      .catch(() => {
        if (cancelled) {
          return;
        }
        setFetchedVariables({ datasetId, failed: true, options: [] });
      });

    return () => {
      cancelled = true;
    };
  }, [datasource, datasetId]);

  const variablesAreCurrent = !!datasetId && fetchedVariables.datasetId === datasetId;
  const variableOptions = variablesAreCurrent ? fetchedVariables.options : [];
  const variablesLoading = !!datasetId && !variablesAreCurrent;
  const variablesError = variablesAreCurrent && fetchedVariables.failed ? VARIABLES_ERROR : undefined;

  const loadDatasets = useCallback(
    async (inputValue: string): Promise<Array<ComboboxOption<string>>> => {
      const response = await datasource.searchDatasets(inputValue);
      return (response?.datasets ?? []).map(toDatasetOption);
    },
    [datasource]
  );

  const onDatasetIdChange = (option: ComboboxOption<string> | null) => {
    const nextDatasetId = option?.value ?? '';
    setDatasetOption(option);
    onChange({
      ...query,
      datasetId: nextDatasetId,
      // Variables from the previous dataset are guaranteed to error on a new one.
      variables: nextDatasetId === (datasetId ?? '') ? variables : '',
    });
    onRunQuery();
  };

  const onVariablesChange = (options: Array<ComboboxOption<string>>) => {
    onChange({ ...query, variables: options.map((option) => option.value).join(',') });
    onRunQuery();
  };

  const onConstraintsChange = (event: ChangeEvent<HTMLInputElement>) => {
    onChange({ ...query, constraints: event.target.value });
  };

  // A saved dashboard only carries the ID, so fall back to showing that.
  const datasetValue = datasetId
    ? datasetOption?.value === datasetId
      ? datasetOption
      : { value: datasetId, label: datasetId }
    : null;

  return (
    <Stack direction="column">
      <InlineField label="Dataset ID" labelWidth={16} tooltip="Search the server's datasets, or type an ID directly">
        <Combobox
          id="query-editor-dataset-id"
          options={loadDatasets}
          value={datasetValue}
          onChange={onDatasetIdChange}
          onBlur={onRunQuery}
          isClearable
          createCustomValue
          customValueDescription="Use this dataset ID"
          placeholder="Search datasets"
        />
      </InlineField>
      <InlineField
        label="Variables"
        labelWidth={16}
        tooltip="Variables to request; time is added automatically"
        invalid={variablesError !== undefined}
        error={variablesError}
      >
        <MultiCombobox
          id="query-editor-variables"
          options={variableOptions}
          value={splitVariables(variables)}
          onChange={onVariablesChange}
          onBlur={onRunQuery}
          disabled={!datasetId}
          loading={variablesLoading}
          invalid={variablesError !== undefined}
          createCustomValue
          customValueDescription="Use this variable name"
          placeholder={datasetId ? 'Select variables' : 'Select a dataset first'}
        />
      </InlineField>
      <InlineField label="Constraints" labelWidth={16} tooltip='Optional, e.g. station="A01"&depth<2'>
        <Input
          id="query-editor-constraints"
          onChange={onConstraintsChange}
          onBlur={onRunQuery}
          value={constraints || ''}
        />
      </InlineField>
    </Stack>
  );
}
