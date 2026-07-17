---
name: semantic-frontend-purist
description: Triggered when writing HTML layouts, structuring web documents, defining class names, or editing visual style rules. Use this to ensure accessible, semantic web layouts.
---

# Semantic Frontend Purist

## Goal
Ensure all user interfaces are structurally accessible, highly legible, and strictly decoupled from presentation logic using native HTML5 and plain CSS.

## Instructions
1. **Structural Priority:** Prioritize explicit structural HTML elements (e.g., `<main>`, `<nav>`, `<article>`, `<header>`, `<section>`, `<aside>`) to describe the document's content hierarchy rather than nesting generic layout utilities like `<div>`.
2. **Intentional Naming:** Choose CSS class names that reflect functional intent or domain meaning (e.g., `.user-profile-card`, `.nav-menu-toggle`) rather than visual presentation or layout shortcuts.
3. **Decoupled Architecture:** Keep presentation logic entirely inside dedicated `.css` sheets. Maintain a clear separation between the document layout rules and the application logic.

## Constraints
* **Inline Style Ban:** Never insert inline `style="..."` attributes under any circumstances.
* **No Utility Frameworks or CSS-in-JS:** Utility-first utility engines or CSS-in-JS runtimes are prohibited. Code styling using plain, unadulterated CSS rule declarations.
