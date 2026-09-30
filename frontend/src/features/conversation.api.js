import api from "../utils/axios";

export const getConversations = async () => {
  const { data } = await api.get("/api/chat/conversations");
  return data;
};

export const createConversation = async () => {
  const { data } = await api.post("/api/chat/conversations", {});
  return data;
};

export const renameConversation = async (conversationId, title) => {
  const { data } = await api.patch(`/api/chat/conversations/${conversationId}`, { title });
  return data;
};

export const deleteConversation = async (conversationId) => {
  await api.delete(`/api/chat/conversations/${conversationId}`);
};
