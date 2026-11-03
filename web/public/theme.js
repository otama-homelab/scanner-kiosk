'use strict';
(()=>{
 const key='scanner.theme';
 const media=matchMedia('(prefers-color-scheme: dark)');
 let choice='system';
 try{const saved=localStorage.getItem(key);if(['light','dark','system'].includes(saved))choice=saved;}catch{}
 function apply(){
  const resolved=choice==='system'?(media.matches?'dark':'light'):choice;
  document.documentElement.dataset.theme=resolved;
  document.documentElement.style.colorScheme=resolved;
  document.querySelectorAll('[data-theme-choice]').forEach(b=>b.setAttribute('aria-pressed',String(b.dataset.themeChoice===choice)));
 }
 apply();
 media.addEventListener('change',apply);
 document.addEventListener('DOMContentLoaded',()=>{
  apply();document.querySelectorAll('[data-theme-choice]').forEach(b=>b.addEventListener('click',()=>{
   choice=b.dataset.themeChoice;try{localStorage.setItem(key,choice);}catch{}apply();
  }));
 });
})();
