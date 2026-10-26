export function resolveMediaUrl(value: string): string {
  try {
    const url = new URL(value)
    if (
      (url.hostname === 'localhost' || url.hostname === '127.0.0.1') &&
      url.port === '9000' &&
      url.pathname.startsWith('/twitter-media/')
    ) {
      return `/media${url.pathname}${url.search}`
    }
  } catch {
    // Relative same-origin media URLs are already usable.
  }
  return value
}
