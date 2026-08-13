import React, { ChangeEvent } from 'react';
import { QueryEditorProps } from '@grafana/data';
import { InlineField, Input, Stack } from '@grafana/ui';

import type { DataSource } from './datasource';
import { ErddapDataSourceOptions, ErddapQuery, ErddapVariableQuery } from './types';

type Props = QueryEditorProps<DataSource, ErddapQuery, ErddapDataSourceOptions, ErddapVariableQuery>;

/**
 * Editor for a dashboard query variable.
 *
 * Deliberately plain text inputs rather than the panel query editor's discovery
 * pickers: the fields here routinely hold references to *other* variables
 * (`$dataset`), which a picker backed by a live dataset list cannot offer.
 */
export function VariableQueryEditor({ query, onChange, onRunQuery }: Props) {
  const { datasetId, variable, constraints } = query;

  const update = (field: keyof ErddapVariableQuery) => (event: ChangeEvent<HTMLInputElement>) => {
    onChange({ ...query, [field]: event.target.value });
  };

  return (
    <Stack direction="column">
      <InlineField label="Dataset ID" labelWidth={16} tooltip="The tabledap dataset ID, e.g. M01_sbe37_all">
        <Input
          id="variable-editor-dataset-id"
          onChange={update('datasetId')}
          onBlur={onRunQuery}
          value={datasetId || ''}
          placeholder="M01_sbe37_all"
        />
      </InlineField>
      <InlineField label="Variable" labelWidth={16} tooltip="The variable whose distinct values become the options">
        <Input
          id="variable-editor-variable"
          onChange={update('variable')}
          onBlur={onRunQuery}
          value={variable || ''}
          placeholder="station"
        />
      </InlineField>
      <InlineField
        label="Constraints"
        labelWidth={16}
        tooltip="Optional; the dashboard time range is not applied, e.g. time>=2024-01-01"
      >
        <Input
          id="variable-editor-constraints"
          onChange={update('constraints')}
          onBlur={onRunQuery}
          value={constraints || ''}
        />
      </InlineField>
    </Stack>
  );
}
