/**
 * Validates the ERDDAP base URL entered in the config editor.
 *
 * Only shape is checked here; whether the server actually answers is left to
 * the backend's CheckHealth ("Save & test").
 *
 * @param value raw value from the input
 * @returns an error message, or undefined when the value is acceptable
 */
export function validateBaseUrl(value: string): string | undefined {
  const trimmed = value.trim();

  if (!trimmed) {
    return 'ERDDAP URL is required';
  }

  let url: URL;
  try {
    url = new URL(trimmed);
  } catch {
    return 'Enter a full URL, for example https://data.neracoos.org/erddap';
  }

  if (url.protocol !== 'http:' && url.protocol !== 'https:') {
    return 'URL must start with http:// or https://';
  }

  return undefined;
}

/**
 * Canonicalizes the ERDDAP base URL so it matches what the backend expects.
 *
 * Mirrors `strings.TrimRight(settings.BaseURL, "/")` in pkg/models/settings.go.
 *
 * @param value raw value from the input
 * @returns the trimmed value without trailing slashes
 */
export function normalizeBaseUrl(value: string): string {
  return value.trim().replace(/\/+$/, '');
}
