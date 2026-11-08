import type {Key} from './messages';

const trash = '<path d="M3 6h18M9 6V3h6v3M5 6l1 15h12l1-15M10 10v7M14 10v7"/>';
const iconPaths: Partial<Record<Key, string>> = {
 rotate: '<path d="M20 7v5h-5M20 12a8 8 0 1 0-2 5"/>',
 moveEarlier: '<path d="m6 12 6-6 6 6M12 6v14"/>',
 moveLater: '<path d="m6 12 6 6 6-6M12 4v14"/>',
 correctionCompare: '<path d="M4 7h16M7 4v6M17 4v6M4 17h16M7 14v6M17 14v6"/>',
 deletePage: trash,
 deleteDocument: trash,
 gridView: '<rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/>',
 listView: '<path d="M9 5h12M9 12h12M9 19h12M3 5h1M3 12h1M3 19h1"/>',
};
export function iconButton(control: HTMLButtonElement, shape: Key) {
 const path = iconPaths[shape];
 if (!path) throw new Error('Invalid icon');
 const label = control.textContent || '';
 control.title = label;
 control.setAttribute('aria-label', label);
 const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
 for (const [name, value] of Object.entries({
  viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor',
  'stroke-width': '1.7', 'stroke-linecap': 'round', 'stroke-linejoin': 'round',
  'aria-hidden': 'true',
 })) svg.setAttribute(name, value);
 svg.innerHTML = path;
 control.replaceChildren(svg);
 control.classList.add('icon-button');
 return control;
}
