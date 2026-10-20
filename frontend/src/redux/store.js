import { configureStore } from '@reduxjs/toolkit'
import  userSlice  from './user.slice'
import  conversationSlice  from './conversation.slice'
import  messageSlice  from './message.slice'
import  uiSlice  from './ui.slice'

export const store = configureStore({
  reducer: {
    user:userSlice,
    conversation:conversationSlice,
    message:messageSlice,
    ui:uiSlice
  },
})
