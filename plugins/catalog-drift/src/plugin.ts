import {
  createPlugin,
  createRoutableExtension,
  createRouteRef,
} from '@backstage/core-plugin-api';

/** Route for the governance policy admin page. */
export const governancePageRouteRef = createRouteRef({
  id: 'catalog-drift:governance',
});

/** Route for the individual policy detail page. */
export const policyDetailRouteRef = createRouteRef({
  id: 'catalog-drift:policy-detail',
  params: ['namespace', 'name'],
});

export const catalogDriftPlugin = createPlugin({
  id: 'catalog-drift',
  routes: {
    governancePage: governancePageRouteRef,
    policyDetail: policyDetailRouteRef,
  },
});

/**
 * GovernancePolicyPage — full-page admin view listing all GovernancePolicy
 * entities and allowing admins to create/edit policies.
 *
 * Mount at a route in your App:
 *
 * ```tsx
 * <Route path="/catalog-drift" element={<GovernancePolicyPage />} />
 * ```
 */
export const GovernancePolicyPage = catalogDriftPlugin.provide(
  createRoutableExtension({
    name: 'GovernancePolicyPage',
    component: () =>
      import('./components/GovernancePolicyPage').then(
        m => m.GovernancePolicyPage,
      ),
    mountPoint: governancePageRouteRef,
  }),
);

/**
 * ApiGovernanceCard — summary card shown on an API entity page displaying the
 * active governance policy and deprecation timeline for that API.
 *
 * Add to the API entity page layout:
 *
 * ```tsx
 * <EntityLayout.Route path="/" title="Overview">
 *   <Grid container spacing={3}>
 *     ...
 *     <Grid item md={6}><ApiGovernanceCard /></Grid>
 *   </Grid>
 * </EntityLayout.Route>
 * ```
 */
export const ApiGovernanceCard = catalogDriftPlugin.provide(
  createRoutableExtension({
    name: 'ApiGovernanceCard',
    component: () =>
      import('./components/ApiGovernanceCard').then(m => m.ApiGovernanceCard),
    mountPoint: governancePageRouteRef,
  }),
);
