export const methods = Object.freeze(['networks','accounts','status','create','import','unlock','lock','balances','receive','history','prepare','send','retry','reconcile','confirmRecovery','backupPhrase','password','addAccount','copyReceive','saveQR','scanQR','configure','associate','chainHistory','max','discovery','hideAccount','discoverAccounts','cancelPrepared','exchangeStatus','exchangeConnect','exchangeDisconnect','exchangeQuote','exchangeDeposit','exchangeSwap','exchangeWithdraw','exchangeReconcile'] as const);
export type Method = typeof methods[number];
const exchangeEndpoints = new Set(['https://api.bybit.com','https://api.bybit.ae','https://api.bytick.com','https://api.bybit.tr','https://api.bybit.kz','https://api.bybitgeorgia.ge','https://api.bybit.eu','https://api.bybit.id','https://api.manepa.jp','https://api.spark-fintech.com']);
const fields: Record<Method, Record<string, 'string'|'index'|'words'|'height'|'gap'|'limit'|'budget'|'boolean'|'cursor'|'optionalString'|'side'|'decimal'|'slippage'|'endpoint'>> = {
 exchangeStatus:{},exchangeConnect:{endpoint:'endpoint'},exchangeDisconnect:{},exchangeQuote:{accountId:'string',assetId:'string',side:'side'},exchangeDeposit:{accountId:'string',assetId:'string',amount:'decimal'},exchangeSwap:{accountId:'string',assetId:'string',side:'side',amount:'decimal',slippage:'slippage'},exchangeWithdraw:{accountId:'string',assetId:'string',amount:'decimal'},exchangeReconcile:{id:'string'},
 cancelPrepared:{id:'string'},hideAccount:{accountId:'string',hidden:'boolean'},discoverAccounts:{start:'index',end:'index'},max:{accountId:'string',asset:'string',recipient:'string',amount:'string',memo:'string'},discovery:{accountId:'string',birthday:'height',gap:'gap',limit:'limit',blockBudget:'budget'},chainHistory:{accountId:'string',cursor:'cursor'},configure:{network:'string'},associate:{accountId:'string',address:'string'},copyReceive:{accountId:'string'},saveQR:{accountId:'string'},scanQR:{},networks:{},accounts:{cursor:'cursor'},status:{},create:{words:'words'},import:{words:'words'},unlock:{},lock:{},balances:{accountId:'string'},receive:{accountId:'string'},history:{accountId:'optionalString',asset:'optionalString',cursor:'cursor'},prepare:{accountId:'string',asset:'string',recipient:'string',amount:'string',memo:'string'},send:{id:'string'},retry:{id:'string'},reconcile:{id:'string'},confirmRecovery:{},backupPhrase:{},password:{},addAccount:{index:'index'}
};
export function validate(method: unknown, body: unknown): asserts method is Method {
 if (typeof method !== 'string' || !Object.hasOwn(fields, method)) throw new Error('Unsupported operation');
 if (body === null || typeof body !== 'object' || Array.isArray(body)) throw new Error('Invalid request');
 const schema=fields[method as Method]; const value=body as Record<string,unknown>;
 if (Object.keys(value).some(key=>!Object.hasOwn(schema,key))||Object.entries(schema).some(([key,type])=>type!=='cursor'&&type!=='optionalString'&&!Object.hasOwn(value,key))) throw new Error('Invalid fields');
 for(const [key,type] of Object.entries(schema)) {
  const input=value[key];
  if(type==='endpoint'&&(typeof input!=='string'||!exchangeEndpoints.has(input)))throw new Error('Invalid exchange endpoint');
  if(type==='side'&&input!=='Buy'&&input!=='Sell')throw new Error('Invalid exchange direction');
  if(type==='slippage'&&(typeof input!=='string'||!/^(0\.[0-9]{1,2}|1(\.0{1,2})?)$/.test(input)||!/[1-9]/.test(input)))throw new Error('Invalid slippage limit');
  if(type==='decimal'&&(typeof input!=='string'||input.length>128||!/^(0|[1-9][0-9]*)(\.[0-9]+)?$/.test(input)||!/[1-9]/.test(input)))throw new Error('Invalid exact decimal');
  if(type==='optionalString'&&Object.hasOwn(value,key)&&(typeof input!=='string'||input.length>4096))throw new Error('Invalid public field');
  if(type==='cursor' && Object.hasOwn(value,key) && (typeof input!=='string'||!/^[0-9a-f]{32}$/.test(input)))throw new Error('Invalid continuation');
  if(type==='string' && (typeof input!=='string'||input.length>4096)) throw new Error('Invalid public field');
  if(type==='index' && (!Number.isInteger(input)||Number(input)<0||Number(input)>=1000000)) throw new Error('Invalid index');
  if(type==='height'&&(!Number.isInteger(input)||Number(input)<0||Number(input)>4294967295))throw new Error('Invalid block height');
  if(type==='gap'&&(!Number.isInteger(input)||Number(input)<1||Number(input)>100))throw new Error('Invalid gap');
  if(type==='limit'&&(!Number.isInteger(input)||Number(input)<1||Number(input)>10000))throw new Error('Invalid discovery limit');
  if(type==='budget'&&(!Number.isInteger(input)||Number(input)<0||Number(input)>2000000))throw new Error('Invalid block budget');
  if(type==='boolean'&&typeof input!=='boolean')throw new Error('Invalid boolean');
  if(type==='words' && input!==12&&input!==24) throw new Error('Invalid word count');
 }
}
