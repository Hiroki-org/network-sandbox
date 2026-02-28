export const API_BASE = process.env.REACT_APP_API_URL || "";

export const apiUrl = (path: string) => `${API_BASE}${path}`;
