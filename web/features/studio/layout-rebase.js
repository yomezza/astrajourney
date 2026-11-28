const conflict = Symbol("workspace conflict");
const encode = value => JSON.stringify(value,(_key,item) =>
  item && typeof item === "object" && !Array.isArray(item) ?
    Object.fromEntries(Object.entries(item).sort(([left],[right]) => left.localeCompare(right))) : item);
const equal = (left,right) => encode(left) === encode(right);

function choose(base, local, remote) {
  if (equal(local,base)) return remote;
  if (equal(remote,base) || equal(local,remote)) return local;
  return conflict;
}

function mergeRecords(base,local,remote,merge) {
  const ids = list => list.map(item => item.id);
  const order = choose(ids(base),ids(local),ids(remote));
  if (order === conflict) return conflict;
  const index = list => new Map(list.map(item => [item.id,item]));
  const [before,draft,latest] = [base,local,remote].map(index);
  const values = new Map();
  for (const id of new Set([...before.keys(),...draft.keys(),...latest.keys()])) {
    const entries = [before.get(id),draft.get(id),latest.get(id)];
    const item = merge && entries.every(Boolean) ? merge(...entries) : choose(...entries);
    if (item === conflict || (order.includes(id) !== !!item)) return conflict;
    if (item) values.set(id,item);
  }
  return order.map(id => values.get(id));
}

function fields(base,local,remote,skip = []) {
  const output = {};
  for (const key of new Set([...Object.keys(base),...Object.keys(local),...Object.keys(remote)])) {
    if (skip.includes(key)) continue;
    const value = choose(base[key],local[key],remote[key]);
    if (value === conflict) return conflict;
    if (value !== undefined) output[key] = value;
  }
  return output;
}

function mergeModule(base,local,remote) {
  const binding = module => [module.binding,module.ship_id];
  const context = choose(binding(base),binding(local),binding(remote));
  const result = fields(base,local,remote,["binding","ship_id"]);
  if (context === conflict || result === conflict) return conflict;
  return {...result,binding:context[0],ship_id:context[1]};
}

export function rebaseLayout(base,local,remote) {
  if (!base || base.version !== local.version || local.version !== remote.version) return null;
  const top = fields(base,local,remote,["workspaces"]);
  const workspaces = mergeRecords(base.workspaces,local.workspaces,remote.workspaces,(before,draft,latest) => {
    const result = fields(before,draft,latest,["modules"]);
    const modules = mergeRecords(before.modules,draft.modules,latest.modules,mergeModule);
    return result === conflict || modules === conflict ? conflict : {...result,modules};
  });
  if (top === conflict || workspaces === conflict || !workspaces.some(({id}) => id === top.active) ||
      !workspaces.some(({id}) => id === top.default)) return null;
  return structuredClone({...top,workspaces});
}
