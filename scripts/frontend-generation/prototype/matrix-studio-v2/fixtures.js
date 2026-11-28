// All objects belong to the isolated demo. No network or game commands.
export const data = {
  ships: {
    watch: { id: "watch", name: "Дозор · #41", model: "Разведчик", fuel: 68, tank: 100, hold: 60,
      system: "Эдем", dock: "beacon", base: { tank: 90, hold: 60 }, equipment: ["Топливный бак B", "Сканер A"],
      cargo: [["Водород", "Топливо", 12], ["Ферросиликаты", "Ресурсы", 24]] },
    relay: { id: "relay", name: "Караван · #82", model: "Грузовой", fuel: 40, tank: 80, hold: 120,
      system: "Альтаир", base: { tank: 80, hold: 100 }, equipment: ["Грузовой отсек B"],
      cargo: [["Водяной лёд", "Ресурсы", 48], ["Титановая руда", "Ресурсы", 18]] },
  },
  stations: {
    beacon: { name: "Маяк · #104", personal: [["Аргон", "Ресурсы", 84]], corporate: [["Ферросиликаты", "Ресурсы", 250]] },
    gate: { name: "Врата · #208", personal: [["Водяной лёд", "Ресурсы", 18]] },
  },
  systems: ["Эдем", "Тихо", "Альтаир", "Гальцион"],
  lanes: [["Эдем", "Тихо"], ["Тихо", "Альтаир"], ["Альтаир", "Гальцион"]],
  local: { "Эдем": ["Маяк", "Орбита Эдема", "Месторождение"], "Альтаир": ["Врата", "Орбита Альтаира"] },
};
