# NavalPlan Frontend Style Guide

**Goal:** Ensure `navalplan` maintains strict visual parity with its peer application, `navallog`.
**Design Philosophy:** "Nautical Glassmorphism." The UI floats above a full-screen map using semi-transparent backgrounds with background blurs, earthy brand colors, and strong typography.

---

## 1. External Dependencies & Assets

### 1.1 Typography

We use Google Fonts. You must include these in the `<head>` of your `index.html`.

* **Primary (Headings/UI):** `Raleway` (Weights: 900)
* **Secondary (Body/Details):** `Lato` (Weights: 400, 700, 900)

**Import Link:**

```html
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Lato:ital,wght@0,100;0,300;0,400;0,700;0,900;1,100;1,300;1,400;1,700;1,900&family=Raleway:ital,wght@0,100..900;1,100..900&display=swap" rel="stylesheet">
```

### 1.2 Icons

We use **Google Material Symbols Outlined**.

**Import Link:**

```html
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Material+Symbols+Outlined:opsz,wght,FILL,GRAD@20..48,100..700,0..1,-50..200" />
```

---

## 2. Design Tokens (CSS Variables)

Copy this `:root` definition into `css/base.css`. This defines the earthy/nautical color palette.

```css
:root {
  /* --- Brand Colors (Earthy/Nautical) --- */
  --brand-dark: rgb(88, 61, 27);       /* Deep Brown */
  --brand-darker: rgb(55, 38, 17);     /* Almost Black Brown */
  --brand-medium: rgb(149, 104, 45);   /* Gold/Bronze */
  --brand-light: rgb(160, 144, 123);   /* Sand */
  --brand-lighter: rgb(227, 220, 211);
  --brand-green-dark: #314c3b;         /* Forest Green */
  --brand-green-darker: #253b2e;
  
  /* --- UI Colors --- */
  --color-background: #AADAFF;         /* Google Maps Water Blue */
  --text-dark: #333333;
  --text-light: #777777;
  --text-subtle: rgba(153, 153, 153, 0.5);
  --border-color: #cccccc;
  
  /* --- Surface (Glassmorphism) --- */
  --surface-bg: rgba(255, 255, 255, 0.8);
  --surface-bg-dim: rgba(255, 255, 255, 0.8);
  --surface-bg-dim-light: rgba(255, 255, 255, 0.4);

  /* --- Dimensions & Effects --- */
  --border-radius: 8px;
  --shadow-main: 0 3px 10px rgb(0 0 0 / 0.4);
  
  /* --- Fonts --- */
  --font-primary: "Raleway", sans-serif;
  --font-secondary: "Lato", sans-serif;
}
```

---

## 3. Layout Architecture

The application uses a **Z-Layered Approach**.

1. **Layer 0 (Map):** The Google Maps canvas occupies 100% width/height absolute.
2. **Layer 1 (Floating Panels):** UI elements are `position: absolute` floating *over* the map, not in a grid.

### CSS Reset & Base

```css
html, body {
  padding: 0; margin: 0;
  background-color: var(--color-background);
  overflow: hidden; /* Prevent scrolling, let map handle it */
  width: 100%; height: 100%;
}
```

---

## 4. UI Components

### 4.1 The "Glass" Panel

All sidebars, modals, and headers use a specific glass effect.

* **Background:** `var(--surface-bg)` (80% opacity white)
* **Blur:** `backdrop-filter: blur(10px)`
* **Border:** `2px solid var(--border-color)`
* **Shadow:** `var(--shadow-main)`
* **Radius:** `var(--border-radius)`

**Usage Example:**

```css
.panel {
  position: absolute;
  background-color: var(--surface-bg);
  backdrop-filter: blur(10px);
  border: 2px solid var(--border-color);
  border-radius: var(--border-radius);
  box-shadow: var(--shadow-main);
}
```

### 4.2 Buttons (`.button-base`)

Buttons are bold, distinct, and share the glass effect.

```css
.button-base {
  font-family: var(--font-primary);
  font-weight: 900; /* Heavy weight is key to the look */
  text-transform: uppercase; /* Often used */
  background-color: var(--surface-bg-dim);
  backdrop-filter: blur(10px);
  border: 1px solid grey;
  border-radius: var(--border-radius);
  box-shadow: 0 2px 8px rgb(0 0 0 / 0.2);
  cursor: pointer;
  padding: 0.5rem 1rem;
}

.button-base:hover {
  background-color: rgba(0, 0, 0, 0.08); /* Subtle darken on hover */
}
```

---

## 5. Map Styling (Google Maps)

We use `google.maps.marker.AdvancedMarkerElement` and `PinElement` for consistency with modern web standards.

### 5.1 Voyage Stop Markers
* **Type:** `PinElement`
* **Color:** `#EA4335` (Google Maps Red)
* **Label:** Numerical index (1, 2, 3...)

### 5.2 Facility & Recommendation Markers
* **Type:** Custom HTML content inside `AdvancedMarkerElement`.
* **Style:** Circular colored background with white Material Symbols icon.

---

## 6. Layout Configuration (Sidebar + Map)

The application adopts a **Left-Sidebar / Full-Map** layout.

* **Sidebar (`#sidebar`):**
* Width: `500px` (Desktop).
* Position: `top: 10px`, `left: 10px`, `bottom: 10px`.
* Content: Tabs (Voyages / Itinerary), Research Ticker.


* **Mobile Responsiveness:**
* Sidebar becomes full-screen drawer (slides from left).
* Floating buttons for map interaction.



## 7. Implementation Checklist

1. [x] Create `css/base.css` with the Variables block.
2. [x] Create `css/components/buttons.css` with `.button-base` styles.
3. [x] Create `css/layout.css` ensuring `#map` is z-index 1 and panels are z-index 2+.
4. [x] Update `index.html` to import Raleway and Lato.
5. [x] Ensure all "White" backgrounds use `var(--surface-bg)` to maintain the glass look.
6. [x] Use `Raleway` 900 weight for all Headers and Buttons.
