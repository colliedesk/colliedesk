import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { App } from './app';
import { loadLang } from './lang';
import './operator.css';

void loadLang().then(() => createRoot(document.getElementById('root')!).render(<StrictMode><App /></StrictMode>));
