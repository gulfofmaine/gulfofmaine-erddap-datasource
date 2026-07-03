import React, { ChangeEvent } from 'react';
import { InlineField, Input, Stack } from '@grafana/ui';
import { QueryEditorProps } from '@grafana/data';
import { DataSource } from '../datasource';
import { ErddapDataSourceOptions, ErddapQuery } from '../types';

type Props = QueryEditorProps<DataSource, ErddapQuery, ErddapDataSourceOptions>;

export function QueryEditor({ query, onChange, onRunQuery }: Props) {
  const onDatasetIdChange = (event: ChangeEvent<HTMLInputElement>) => {
    onChange({ ...query, datasetId: event.target.value });
  };

  const onVariablesChange = (event: ChangeEvent<HTMLInputElement>) => {
    onChange({ ...query, variables: event.target.value });
  };

  const onConstraintsChange = (event: ChangeEvent<HTMLInputElement>) => {
    onChange({ ...query, constraints: event.target.value });
  };

  const { datasetId, variables, constraints } = query;

  return (
    <Stack direction="column">
      <InlineField label="Dataset ID" labelWidth={16}>
        <Input id="query-editor-dataset-id" onChange={onDatasetIdChange} onBlur={onRunQuery} value={datasetId || ''} />
      </InlineField>
      <InlineField
        label="Variables"
        labelWidth={16}
        tooltip="Comma-separated variable names; time is added automatically"
      >
        <Input id="query-editor-variables" onChange={onVariablesChange} onBlur={onRunQuery} value={variables || ''} />
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
