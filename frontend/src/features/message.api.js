import api from "../utils/axios";

export const getMessages = async (conversationId) => {
  const { data } = await api.get(`/api/chat/conversations/${conversationId}/messages`);
  return data;
};
