// SPDX-License-Identifier: MIT
// Adapted from shadcn/ui Button and ButtonGroup; see web/vendor/shadcn-ui/.
import {cva,type VariantProps} from 'class-variance-authority';

const buttonVariants=cva('ui-button',{variants:{variant:{outline:'ui-button-outline',ghost:'ui-button-ghost'},size:{default:'ui-button-default',icon:'ui-button-icon'}},defaultVariants:{variant:'outline',size:'default'}});

export function uiButton(label:string,options:VariantProps<typeof buttonVariants>={}){
 const element=document.createElement('button');element.type='button';element.dataset.slot='button';element.className=buttonVariants(options);element.textContent=label;return element;
}

export function uiButtonGroup(label:string,...children:HTMLElement[]){
 const element=document.createElement('div');element.className='ui-button-group';element.dataset.slot='button-group';element.setAttribute('role','group');element.setAttribute('aria-label',label);element.append(...children);return element;
}
