import { createSlice } from "@reduxjs/toolkit";

// UI state shared across components: the empty state's specialist list and the
// composer both pick the agent, and the top bar opens sheets the sidebar owns.
const initialState = {
  agent: "auto",
  navOpen: false, // the sidebar sheet on phones
  codeOpen: false, // the code / preview sheet on phones
  billingOpen: false,
};

export const uiSlice = createSlice({
  name: "ui",
  initialState,
  reducers: {
    setAgent: (state, action) => {
      state.agent = action.payload;
    },
    setNavOpen: (state, action) => {
      state.navOpen = action.payload;
    },
    setCodeOpen: (state, action) => {
      state.codeOpen = action.payload;
    },
    setBillingOpen: (state, action) => {
      state.billingOpen = action.payload;
    },
  },
});

export const { setAgent, setNavOpen, setCodeOpen, setBillingOpen } = uiSlice.actions;

export default uiSlice.reducer;
