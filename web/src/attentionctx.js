import { createContext, useContext } from 'react';

// The shared list of what needs the person. It lives in its own file so the tab bar can read it
// without importing the code that works it out.
export const AttentionContext = createContext({ items: [], byPage: {}, byTab: {}, loaded: false, worst: '' });
export const useAttention = () => useContext(AttentionContext);
