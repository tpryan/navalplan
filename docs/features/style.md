# NavalPlan Frontend Style Guide

**Goal:** Ensure `navalplan` maintains strict visual parity with its peer application, `navallog`.
**Design Philosophy:** "Nautical Glassmorphism." The UI floats above a full-screen map using semi-transparent backgrounds with background blurs, earthy brand colors, and strong typography.

---

## 1. External Dependencies & Assets

### 1.1 Typography

We use Google Fonts. You must include these in the `<head>` of your `index.html`.

* **Primary (Headings/UI):** `Raleway` (Weights: 900)
* **Secondary (Body/Details):** `Lato` (Weights: 900 - *Note: The CSS implies bold usage*)
* **Accent (Map Markers):** `Sue Ellen Francisco` (Cursive/Handwritten)

**Import Link:**

```html
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Sue+Ellen+Francisco&family=Lato:wght@900&family=Raleway:wght@900&display=swap" rel="stylesheet">

```

### 1.2 Icons

We use **Google Material Symbols Outlined**.

**Import Link:**

```html
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Material+Symbols+Outlined:opsz,wght,FILL,GRAD@40,700,0,0" />

```

---

## 2. Design Tokens (CSS Variables)

Copy this `:root` definition into `css/base.css`. This defines the earthy/nautical color palette.

```css
:root {
  /* --- Brand Colors (Earthy/Nautical) --- */
  --brand-dcark: rgb(88, 61, 27);       /* Deep Brown */
  --brand-darker: rgb(55, 38, 17);     /* Almost Black Brown */
  --brand-medium: rgb(149, 104, 45);   /* Gold/Bronze */
  --brand-light: rgb(160, 144, 123);   /* Sand */
  --brand-green-dark: #314c3b;         /* Forest Green */
  --brand-green-darker: #253b2e;
  
  /* --- UI Colors --- */
  --color-background: #9f9a84;         /* Sage Green (Page background fallback) */
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
  --font-handwriting: "Sue Ellen Francisco", cursive;
}

```

---

## 3. Layout Architecture

The application uses a **Z-Layered Approach**.

1. **Layer 0 (Map):** The Mapbox GL canvas occupies 100% width/height absolute.
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

### 4.3 Icon Buttons

Used for actions like "Delete", "Share", "Close".

* **Shape:** Circular (`border-radius: 50%`)
* **Background:** None (transparent) until hovered.
* **Icon Color:** `var(--text-subtle)` or `var(--brand-medium)`.

### 4.4 List Items

Lists (like Voyages or Stops) have a card-like appearance.

```css
.list-item {
  background-color: var(--surface-bg-dim-light);
  border: 1px solid grey;
  border-radius: var(--border-radius);
  margin-bottom: 5px;
  box-shadow: 0 2px 8px rgb(0 0 0 / 0.2);
  display: flex;
  align-items: center;
  height: 45px; /* Fixed height is common in Navallog */
}

.list-item.selected {
  background-color: rgba(149, 104, 45, 0.2); /* Brand medium at 20% */
  border-color: var(--brand-dark);
  border-width: 2px;
}

```

---

## 5. Map Styling (Mapbox)

To match the `navallog` map style, markers should be customized using HTML/CSS rather than default Mapbox SVG pins.

### The "Handwritten" Marker

Navallog uses a distinct "post-it note" style marker.

**HTML Structure:**

```html
<div class="marker">
  <span><b>1</b></span>
</div>

```

**CSS:**

```css
.marker span {
  font-family: var(--font-handwriting); /* Sue Ellen Francisco */
  width: 45px; height: 45px;
  background: rgba(184, 164, 138, 0.8); /* Paper color */
  border: solid 2px #314c3b; /* Brand Green */
  border-radius: 0 70% 70%; /* Teardrop shape */
  transform: rotateZ(-225deg); /* Rotation to point down */
  display: flex; justify-content: center; align-items: center;
  font-size: 1.2rem;
  box-shadow: 0 0 2px black;
}

.marker b {
  transform: rotateZ(225deg); /* Counter-rotate text so it is upright */
}

```

---

## 6. Layout Configuration (Sidebar + Map)

For `navalplan`, you should adopt the **Left-Sidebar / Full-Map** layout found in `navallog`.

* **Sidebar (`#sidebar` or `.tracks-panel`):**
* Width: `500px` (Desktop).
* Position: `top: 10px`, `left: 10px`.
* Content: Tabs (Voyages / Stops), Lists.


* **Mobile Responsiveness:**
* Sidebar moves to bottom (`bottom: 0`, `width: 100%`, `height: 40vh`).
* Header styling changes to a footer bar.



## 7. Implementation Checklist for Agent

1. [ ] Create `css/base.css` with the Variables block.
2. [ ] Create `css/components/buttons.css` with `.button-base` styles.
3. [ ] Create `css/layout.css` ensuring `#map` is z-index 1 and panels are z-index 2+.
4. [ ] Update `index.html` to import the 3 Google Fonts.
5. [ ] Ensure all "White" backgrounds use `var(--surface-bg)` to maintain the glass look.
6. [ ] Use `Raleway` 900 weight for all Headers and Buttons.