import { createSlice } from "@reduxjs/toolkit";

const initialState = {
  messages: [],
  isLoading: false,
  status: "", // live progress text from the agent, e.g. "Searching the web"
  artifacts: [],
  draft: "", // lets suggestion chips fill the input box
};

const last = (state) => state.messages[state.messages.length - 1];

export const messageSlice = createSlice({
  name: "message",
  initialState,
  reducers: {
    setMessages: (state, action) => {
      state.messages = action.payload;
    },
    addMessage: (state, action) => {
      state.messages.push({
        artifacts: [],
        attachments: [],
        images: [],
        sources: [],
        ...action.payload,
      });
    },
    // Streaming: tokens are appended to the assistant message in place.
    appendToLast: (state, action) => {
      const msg = last(state);
      if (msg) msg.content += action.payload;
    },
    patchLast: (state, action) => {
      const msg = last(state);
      if (msg) Object.assign(msg, action.payload);
    },
    pushToLast: (state, action) => {
      const { field, value } = action.payload;
      const msg = last(state);
      if (msg) msg[field] = [...(msg[field] || []), value];
    },
    setIsLoading: (state, action) => {
      state.isLoading = action.payload;
    },
    setStatus: (state, action) => {
      state.status = action.payload;
    },
    setArtifacts: (state, action) => {
      state.artifacts = action.payload || [];
    },
    setDraft: (state, action) => {
      state.draft = action.payload;
    },
  },
});

export const {
  setMessages,
  addMessage,
  appendToLast,
  patchLast,
  pushToLast,
  setIsLoading,
  setStatus,
  setArtifacts,
  setDraft,
} = messageSlice.actions;

export default messageSlice.reducer;
