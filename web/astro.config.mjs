import { defineConfig } from 'astro/config';
export default defineConfig({output:'static',cacheDir:'./.astro/cache',vite:{build:{assetsInlineLimit:0}}});
