export const moduleCargo = ship => (ship?.modules || []).reduce((sum,m)=>sum+(m.installed?0:m.size),0);
export const usedCargo = ship => Object.values(ship?.cargo || {}).reduce((sum,n)=>sum+n,0)+moduleCargo(ship);
