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
    <div>
      {/*
       * invalid is passed (InlineField always overrides its child's own
       * invalid prop with this one via cloneElement, so it has to go here to
       * actually reach the Input), but error is deliberately NOT passed:
       * InlineField only inserts/removes its own built-in message node when
       * BOTH invalid and error are set, and on some Grafana releases still
       * covered by e2e, inserting or removing a DOM node here at the moment
       * "Save & test" is clicked breaks that click entirely — no save, no
       * health check ever fires. Toggling the invalid attribute alone on an
       * always-mounted Input doesn't trigger it; the message text below is
       * rendered through a node that stays permanently mounted instead,
       * toggled only via CSS. Confirmed by direct reproduction against the
       * affected Grafana version, not guessed.
       */}
      <InlineField label="ERDDAP URL" labelWidth={14} invalid={showError}>
        <Input
          id="config-editor-base-url"
          onChange={onBaseUrlChange}
          onBlur={onBaseUrlBlur}
          value={jsonData.baseUrl ?? ''}
          placeholder="https://data.neracoos.org/erddap"
          width={60}
        />
      </InlineField>
      <div
        aria-live="polite"
        style={{ color: '#ff5286', fontSize: 12, marginTop: 4, visibility: showError ? 'visible' : 'hidden' }}
      >
        {error ?? ''}
      </div>
    </div>
  );
}
