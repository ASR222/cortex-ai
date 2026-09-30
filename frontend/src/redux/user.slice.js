import { createSlice } from "@reduxjs/toolkit";

const initialState = {
  userData: null,
  checked: false, // true once we know whether a session exists
};

export const userSlice = createSlice({
  name: "user",
  initialState,
  reducers: {
    setUserData: (state, action) => {
      state.userData = action.payload;
      state.checked = true;
    },
    setCredits: (state, action) => {
      if (state.userData && typeof action.payload === "number") {
        state.userData.credits = action.payload;
      }
    },
  },
});

export const { setUserData, setCredits } = userSlice.actions;

export default userSlice.reducer;
