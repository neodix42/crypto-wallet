export const methods = Object.freeze(['networks','accounts','status','create','import','unlock','lock','balances','receive','history','prepare','send','retry','reconcile','backup','restore','password','addAccount','copyReceive','saveQR','scanQR','configure','associate','chainHistory','max','discovery','hideAccount','discoverAccounts'] as const);
export type Method = typeof methods[number];
const fields: Record<Method, Record<string, 'string'|'index'|'words'|'height'|'gap'|'limit'|'budget'|'boolean'>> = {
 hideAccount:{accountId:'string',hidden:'boolean'},discoverAccounts:{start:'index',end:'index'},max:{accountId:'string',asset:'string',recipient:'string',amount:'string',memo:'string'},discovery:{accountId:'string',birthday:'height',gap:'gap',limit:'limit',blockBudget:'budget'},chainHistory:{accountId:'string'},configure:{network:'string'},associate:{accountId:'string',address:'string'},copyReceive:{accountId:'string'},saveQR:{accountId:'string'},scanQR:{},networks:{},accounts:{},status:{},create:{words:'words'},import:{words:'words'},unlock:{},lock:{},balances:{accountId:'string'},receive:{accountId:'string'},history:{},prepare:{accountId:'string',asset:'string',recipient:'string',amount:'string',memo:'string'},send:{id:'string'},retry:{id:'string'},reconcile:{id:'string'},backup:{},restore:{},password:{},addAccount:{index:'index'}
};
export function validate(method: unknown, body: unknown): asserts method is Method {
 if (typeof method !== 'string' || !Object.hasOwn(fields, method)) throw new Error('Unsupported operation');
 if (body === null || typeof body !== 'object' || Array.isArray(body)) throw new Error('Invalid request');
 const schema=fields[method as Method]; const value=body as Record<string,unknown>;
 if (Object.keys(value).length!==Object.keys(schema).length||Object.keys(value).some(key=>!Object.hasOwn(schema,key))||Object.keys(schema).some(key=>!Object.hasOwn(value,key))) throw new Error('Invalid fields');
 for(const [key,type] of Object.entries(schema)) {
  const input=value[key];
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
