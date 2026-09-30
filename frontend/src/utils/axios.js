import axios from "axios";

// All API calls go to the same origin (/api/...). In development Vite proxies
// /api to the gateway; in production Caddy does. Same-origin means the session
// cookie works without any CORS configuration.
const api = axios.create({
  baseURL: "",
  withCredentials: true,
  headers: {
    // Required by the gateway's CSRF check on state-changing requests.
    "X-Requested-With": "XMLHttpRequest",
  },
});

// Lets the app react to an expired session (e.g. show the login dialog).
let onUnauthorized = () => {};
export const setUnauthorizedHandler = (fn) => {
  onUnauthorized = fn;
};

api.interceptors.response.use(
  (res) => res,
  (error) => {
    if (error.response?.status === 401) onUnauthorized();
    return Promise.reject(error);
  },
);

/** Turns any API error into { title, message } for the banner. */
export const errorInfo = (error) => ({
  title: error?.response?.data?.title || "Something went wrong",
  message: error?.response?.data?.message || "Please try again.",
});

export default api;
