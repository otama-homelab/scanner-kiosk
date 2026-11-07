// Existing behavior regressions open the same disclosure a keyboard/touch user opens.
async function reveal(control){
 control=control.and(control.page().locator('button:not(dialog:not([open]) button)'));
 const modal=control.and(control.page().locator('dialog[open] button'));
 if(await modal.count())return modal;
 await control.waitFor({state:'attached'});
 if(await control.isVisible())return control;
 for(const menu of await control.locator('xpath=ancestor::details').all()){
  if(!await menu.evaluate(element=>element.open))await menu.locator(':scope > summary').click();
 }
 return control;
}
module.exports={reveal};
