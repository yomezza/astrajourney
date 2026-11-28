import assert from "node:assert/strict";
import {test} from "node:test";
import {rebaseLayout} from "../../web/features/studio/layout-rebase.js";

const make = () => ({version:1,active:"one",default:"one",workspaces:[{
  id:"one",name:"One",ship_id:0,modules:[{
    id:"cargo",type:"cargo",binding:"workspace",ship_id:0,geometry:{x:100,y:40},
  }],
}]});

test("layout rebase preserves a moved window and an independent added module",() => {
  const base = make(), mine = structuredClone(base), theirs = structuredClone(base);
  mine.workspaces[0].modules[0].geometry.x = 500;
  theirs.workspaces[0].modules.push({id:"map",type:"map",binding:"none",ship_id:0});
  const merged = rebaseLayout(base,mine,theirs);
  assert.equal(merged.workspaces[0].modules.length,2);
  assert.equal(merged.workspaces[0].modules[0].geometry.x,500);
});

test("layout rebase refuses incompatible bindings and edit-versus-delete",() => {
  const base = make(), mine = structuredClone(base), theirs = structuredClone(base);
  const ours = mine.workspaces[0].modules[0], theirModule = theirs.workspaces[0].modules[0];
  Object.assign(ours,{binding:"fixed",ship_id:1});
  Object.assign(theirModule,{binding:"fixed",ship_id:2});
  assert.equal(rebaseLayout(base,mine,theirs),null);
  theirs.workspaces[0].modules = [];
  assert.equal(rebaseLayout(base,mine,theirs),null);
});
