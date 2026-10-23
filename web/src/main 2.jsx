import React from 'react';
import { createRoot } from 'react-dom/client';
import './styles.css';
import App from './App.jsx';
import { ToastHost } from './ui.jsx';

createRoot(document.getElementById('root')).render(
  <ToastHost>
    <App />
  </ToastHost>,
);
