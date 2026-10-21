import axios from 'axios';

export function getErrorMessage(
  err: unknown,
  fallback = 'Something went wrong',
): string {
  if (axios.isAxiosError(err)) {
    const serverError = err.response?.data?.error;
    if (typeof serverError === 'string') {
      const requestID = err.response?.headers['x-request-id'];
      return requestID ? `${serverError} (${requestID})` : serverError;
    }
    const detail = err.response?.data?.detail;
    if (typeof detail === 'string') return detail;
    if (Array.isArray(detail) && detail.length > 0) {
      const first = detail[0];
      if (typeof first === 'string') return first;
      if (first?.msg) return String(first.msg);
    }
    if (err.message) return err.message;
  }
  if (err instanceof Error && err.message) return err.message;
  if (typeof err === 'string') return err;
  return fallback;
}
