import { api } from '../api.js';
import { directionFindingFromApi, directionFindingToApi } from './direction-findings-api.js';

export const directionFindingsStore = (() => {
  let observations = $state([]);

  return {
    get observations() { return observations; },

    async load() {
      const rows = (await api.get('/direction-findings')) || [];
      observations = rows.map(directionFindingFromApi).filter(Boolean);
      return observations;
    },

    async add(observation) {
      const created = await api.post('/direction-findings', directionFindingToApi(observation));
      const mapped = directionFindingFromApi(created);
      if (mapped) observations = [mapped, ...observations];
      return mapped;
    },

    async remove(id) {
      await api.delete(`/direction-findings/${id}`);
      observations = observations.filter((observation) => observation.id !== id);
    },
  };
})();
