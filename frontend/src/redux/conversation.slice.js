import { createSlice } from "@reduxjs/toolkit";

const initialState = {
  conversations: [],
  selectedConversation: null,
};

export const conversationSlice = createSlice({
  name: "conversation",
  initialState,
  reducers: {
    setConversations: (state, action) => {
      state.conversations = action.payload;
    },
    addConversation: (state, action) => {
      state.conversations.unshift(action.payload);
    },
    removeConversation: (state, action) => {
      state.conversations = state.conversations.filter((c) => c._id !== action.payload);
      if (state.selectedConversation?._id === action.payload) {
        state.selectedConversation = null;
      }
    },
    setSelectedConversation: (state, action) => {
      state.selectedConversation = action.payload;
    },
    setConvTitle: (state, action) => {
      const { conversationId, title } = action.payload;
      state.conversations = state.conversations.map((c) => (c._id === conversationId ? { ...c, title } : c));
      if (state.selectedConversation?._id === conversationId) {
        state.selectedConversation = { ...state.selectedConversation, title };
      }
    },
  },
});

export const { setConversations, addConversation, removeConversation, setSelectedConversation, setConvTitle } =
  conversationSlice.actions;

export default conversationSlice.reducer;
