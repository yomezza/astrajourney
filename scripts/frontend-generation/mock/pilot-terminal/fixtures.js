// Fictional pilot-terminal data only. Never use the real player's active ship.
export const SHIPS = Object.freeze([
  {id:41,name:"Пилигрим",model:"agni",star:"Эдем",fuel:79,fuelMax:100,health:93,shield:88,
   position:"Торговый маяк",capacity:80,cargo:{iron:24,silicon:13,water:8},
   mining:[{id:"a",site:"Рудный пояс Эдема",resource:"iron",rate:7,status:"ready"},
           {id:"b",site:"Каменистая Эдем I",resource:"silicon",rate:3,status:"working"}]},
  {id:82,name:"Пилигрим",model:"agni",star:"Альтаир",fuel:36,fuelMax:100,health:76,shield:91,
   position:"Орбита Альтаира II",capacity:80,cargo:{water:32,biomass:14,copper:5},
   mining:[{id:"c",site:"Ледяной пояс Альтаира",resource:"water",rate:12,status:"working"},
           {id:"d",site:"Планета Альтаир II",resource:"biomass",rate:4,status:"ready"}]}
]);
export const RESOURCES = Object.freeze({
  iron:["Железо","Руда"],silicon:["Кремний","Руда"],water:["Вода","Жидкость"],
  biomass:["Биомасса","Органика"],copper:["Медь","Руда"]
});
export function createFleet(){return new Map(SHIPS.map(ship=>[ship.id,structuredClone(ship)]));}
