import api from "../utils/axios";

export const getMe = async () => {
  const { data } = await api.get("/api/me");
  return data.user;
};

export const login = async (idToken) => {
  const { data } = await api.post("/api/auth/login", { idToken });
  return data.user;
};

export const logout = async () => {
  await api.post("/api/auth/logout");
};
