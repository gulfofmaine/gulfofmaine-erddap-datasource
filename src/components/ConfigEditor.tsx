import React, { ChangeEvent, useState } from 'react';
import { InlineField, Input } from '@grafana/ui';
import { DataSourcePluginOptionsEditorProps } from '@grafana/data';
import { ErddapDataSourceOptions } from '../types';
import { normalizeBaseUrl, validateBaseUrl } from './validation';

interface Props extends DataSourcePluginOptionsEditorProps<ErddapDataSourceOptions> {}

export function ConfigEditor(props: Props) {
  const { onOptionsChange, options } = props;
  const { jsonData } = options;
  // Errors are only worth showing once the user has left the field; a fresh,
  // never-filled-in config page should not open with a red input.
  const [touched, setTouched] = useState(false);

  const setBaseUrl = (baseUrl: string) => {
    onOptionsChange({
      ...options,
      jsonData: {
        ...jsonData,
        baseUrl,
      },
    });
  };

  const onBaseUrlChange = (event: ChangeEvent<HTMLInputElement>) => {
    setBaseUrl(event.target.value);
  };

  const onBaseUrlBlur = (event: React.FocusEvent<HTMLInputElement>) => {
    setTouched(true);

    const normalized = normalizeBaseUrl(event.target.value);
    if (normalized !== event.target.value) {
      setBaseUrl(normalized);
    }
  };

  const error = validateBaseUrl(jsonData.baseUrl ?? '');
  const showError = touched && error !== undefined;

  return (
    <InlineField label="ERDDAP URL" labelWidth={14} required invalid={showError} error={showError ? error : undefined}>
      <Input
        id="config-editor-base-url"
        onChange={onBaseUrlChange}
        onBlur={onBaseUrlBlur}
        value={jsonData.baseUrl ?? ''}
        placeholder="https://data.neracoos.org/erddap"
        width={60}
        invalid={showError}
      />
    </InlineField>
  );
}
