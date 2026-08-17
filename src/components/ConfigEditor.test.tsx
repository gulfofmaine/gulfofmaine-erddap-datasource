import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { DataSourcePluginOptionsEditorProps, DataSourceSettings } from '@grafana/data';

import { ConfigEditor } from './ConfigEditor';
import { ErddapDataSourceOptions } from '../types';

type Props = DataSourcePluginOptionsEditorProps<ErddapDataSourceOptions>;

function renderEditor(baseUrl?: string) {
  const onOptionsChange = jest.fn();
  const options = {
    jsonData: baseUrl === undefined ? {} : { baseUrl },
  } as DataSourceSettings<ErddapDataSourceOptions>;

  const props = { options, onOptionsChange } as unknown as Props;
  render(<ConfigEditor {...props} />);

  return { onOptionsChange, input: screen.getByRole('textbox', { name: /ERDDAP URL/i }) };
}

describe('ConfigEditor', () => {
  it('shows no error on first render even when the URL is empty', () => {
    const { input } = renderEditor();

    // The message node stays permanently mounted (see ConfigEditor.tsx) and
    // is only ever hidden via CSS, never added/removed — so it's present in
    // the DOM from the first render, just not visible yet.
    expect(screen.getByText('ERDDAP URL is required')).not.toBeVisible();
    expect(input).toBeValid();
  });

  it('shows the required error once an empty field has been blurred', () => {
    const { input } = renderEditor('');

    fireEvent.blur(input);

    expect(screen.getByText('ERDDAP URL is required')).toBeInTheDocument();
    expect(input).toBeInvalid();
  });

  it('shows the scheme error for a non-http URL', () => {
    const { input } = renderEditor('ftp://example.org');

    fireEvent.blur(input);

    expect(screen.getByText('URL must start with http:// or https://')).toBeInTheDocument();
    expect(input).toBeInvalid();
  });

  it('shows the parse error for a URL without a scheme', () => {
    const { input } = renderEditor('data.neracoos.org/erddap');

    fireEvent.blur(input);

    expect(screen.getByText('Enter a full URL, for example https://data.neracoos.org/erddap')).toBeInTheDocument();
  });

  it('normalizes a trailing slash away on blur', () => {
    const { input, onOptionsChange } = renderEditor('https://x/erddap/');

    fireEvent.blur(input);

    expect(onOptionsChange).toHaveBeenCalledWith(
      expect.objectContaining({ jsonData: { baseUrl: 'https://x/erddap' } })
    );
  });

  it('does not report an error for a valid URL', () => {
    const { input } = renderEditor('https://data.neracoos.org/erddap');

    fireEvent.blur(input);

    expect(screen.queryByText('ERDDAP URL is required')).not.toBeInTheDocument();
    expect(input).toBeValid();
  });

  it('propagates typed values to onOptionsChange', () => {
    const { input, onOptionsChange } = renderEditor('');

    fireEvent.change(input, { target: { value: 'https://x/erddap' } });

    expect(onOptionsChange).toHaveBeenCalledWith(
      expect.objectContaining({ jsonData: { baseUrl: 'https://x/erddap' } })
    );
  });
});
