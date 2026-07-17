name: frameworkless-frontend-builder
description: Triggered when building, tweaking, styling, or updating web presentation assets, vanilla JavaScript single-page applications, HTML layouts, or custom CSS definitions.

## Goal
To maintain a high-performance Single Page Application (SPA) utilizing framework-free vanilla JavaScript compiled natively with Vite 5.

## Instructions
1. **Semantic Blueprinting:** Leverage semantic HTML elements (`<main>`, `<article>`, `<nav>`, `<header>`) to emphasize contextual hierarchy rather than layout shortcuts.
2. **Asset Organization:** Ensure all compiled assets target the backend's native hosting root (e.g., `static.min/`).
3. **Local Dev Routing:** Ensure local dev loops rely on the Vite development proxy setup to transparently forward `/api` and `/auth` vectors to the active Go backend port.

## Constraints
* **Framework Ban:** Do not implement React, Vue, Svelte, Angular, or external UI rendering libraries.
* **Style Separation:** CSS-in-JS configurations and raw HTML inline style overrides are forbidden. All aesthetic definitions must live strictly in standalone `.css` files.
