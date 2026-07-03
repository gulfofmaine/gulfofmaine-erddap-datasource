import React, { ChangeEvent } from 'react';
import { InlineField, Input } from '@grafana/ui';
import { DataSourcePluginOptionsEditorProps } from '@grafana/data';
import { ErddapDataSourceOptions } from '../types';

interface Props extends DataSourcePluginOptionsEditorProps<ErddapDataSourceOptions> {}

export function ConfigEditor(props: Props) {
  const { onOptionsChange, options } = props;
  const { jsonData } = options;

  const onBaseUrlChange = (event: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      jsonData: {
        ...jsonData,
        baseUrl: event.target.value,
      },
    });
  };

  return (
    <InlineField label="ERDDAP URL" labelWidth={14}>
      <Input
        id="config-editor-base-url"
        onChange={onBaseUrlChange}
        value={jsonData.baseUrl ?? ''}
        placeholder="https://data.neracoos.org/erddap"
        width={60}
      />
    </InlineField>
  );
}
