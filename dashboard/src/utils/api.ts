import axios from 'axios';

export const api = axios.create({ baseURL: '/api/dutybot' });

api.interceptors.response.use(
  (response) => response,
  (error) => {
    if (
      axios.isAxiosError(error) &&
      error.response?.status === 401 &&
      typeof window !== 'undefined' &&
      !['/login', '/register'].includes(window.location.pathname)
    ) {
      window.location.assign('/login');
    }
    return Promise.reject(error);
  },
);
