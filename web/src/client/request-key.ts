// Keys bind one immutable intent to a bounded new-admission window. They are
// never refreshed after an uncertain response; changing a key is a new intent.
let serverTime:number|null=null;
let sampledAt=0;
export function observeRequestTime(date:string|null){
 if(!date)return;
 const parsed=Date.parse(date);
 if(Number.isSafeInteger(parsed)&&parsed>=0&&parsed<1e13){serverTime=parsed;sampledAt=performance.now()}
}
export function requestKey(){
 if(serverTime===null)throw new Error('Server request clock unavailable');
 const issued=Math.floor(serverTime+Math.max(0,performance.now()-sampledAt));
 if(!Number.isSafeInteger(issued)||issued<0||issued>=1e13)throw new Error('Server request clock unavailable');
 return 'rq1_'+String(issued).padStart(13,'0')+'_'+crypto.randomUUID().replaceAll('-','');
}
