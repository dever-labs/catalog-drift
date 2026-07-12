import React, { useEffect, useState } from 'react';
import {
  InfoCard,
  Progress,
  MarkdownContent,
} from '@backstage/core-components';
import { useApi, configApiRef } from '@backstage/core-plugin-api';
import { useEntity } from '@backstage/plugin-catalog-react';
import {
  Chip,
  Divider,
  Stack,
  Typography,
} from '@mui/material';
import {
  ANNOTATION_DEPRECATED_SINCE,
  ANNOTATION_SUNSET_DATE,
  ANNOTATION_DEPRECATION_MESSAGE,
  ANNOTATION_SUCCESSOR,
  ANNOTATION_GOVERNANCE_POLICY,
  GovernancePolicyEntity,
} from '../../types';

/**
 * ApiGovernanceCard displays on an API entity page:
 * - Current deprecation status (deprecated since, sunset date, message, successor)
 * - The active GovernancePolicy for that API's owning component
 *
 * Add to your API entity page in packages/app/src/components/catalog/EntityPage.tsx.
 */
export function ApiGovernanceCard() {
  const { entity } = useEntity();
  const configApi = useApi(configApiRef);
  const backstageUrl = configApi.getString('backend.baseUrl');

  const ann = entity.metadata.annotations ?? {};
  const deprecatedSince = ann[ANNOTATION_DEPRECATED_SINCE];
  const sunsetDate      = ann[ANNOTATION_SUNSET_DATE];
  const deprecationMsg  = ann[ANNOTATION_DEPRECATION_MESSAGE];
  const successor       = ann[ANNOTATION_SUCCESSOR];
  const policyRef       = ann[ANNOTATION_GOVERNANCE_POLICY];

  const isDeprecated = (entity as any).spec?.lifecycle === 'deprecated';

  const [policy, setPolicy] = useState<GovernancePolicyEntity | null | undefined>(undefined);
  const [loadingPolicy, setLoadingPolicy] = useState(true);

  useEffect(() => {
    const namespace = entity.metadata.namespace ?? 'default';
    const name      = policyRef ?? 'default';
    fetch(
      `${backstageUrl}/api/catalog/entities/by-name/governancepolicy/${namespace}/${name}`,
      { headers: { Accept: 'application/json' } },
    )
      .then(r => (r.ok ? r.json() : null))
      .then(setPolicy)
      .catch(() => setPolicy(null))
      .finally(() => setLoadingPolicy(false));
  }, [backstageUrl, entity.metadata.namespace, policyRef]);

  return (
    <InfoCard title="API Governance" subheader="catalog-drift policy">
      <Stack spacing={2}>
        {/* Deprecation status */}
        <div>
          <Typography variant="subtitle2">Deprecation status</Typography>
          <Stack direction="row" spacing={1} sx={{ mt: 0.5 }}>
            <Chip
              size="small"
              label={isDeprecated ? 'Deprecated' : 'Active'}
              color={isDeprecated ? 'warning' : 'success'}
            />
            {deprecatedSince && (
              <Chip size="small" label={`Since ${deprecatedSince}`} variant="outlined" />
            )}
            {sunsetDate && (
              <Chip size="small" label={`Sunset ${sunsetDate}`} color="error" variant="outlined" />
            )}
            {successor && (
              <Chip size="small" label={`Successor: ${successor}`} variant="outlined" />
            )}
          </Stack>
          {deprecationMsg && (
            <Typography variant="body2" sx={{ mt: 1 }}>
              <MarkdownContent content={deprecationMsg} />
            </Typography>
          )}
        </div>

        <Divider />

        {/* Governance policy */}
        <div>
          <Typography variant="subtitle2">Active governance policy</Typography>
          {loadingPolicy ? (
            <Progress />
          ) : policy ? (
            <Stack spacing={0.5} sx={{ mt: 0.5 }}>
              <Typography variant="body2">
                <strong>Name:</strong>{' '}
                {policy.metadata.namespace}/{policy.metadata.name}
              </Typography>
              {policy.spec.deprecation?.errorAfter && (
                <Typography variant="body2">
                  <strong>Error after:</strong> {policy.spec.deprecation.errorAfter}
                </Typography>
              )}
              {policy.spec.deprecation?.warnBeforeSunset && (
                <Typography variant="body2">
                  <strong>Warn before sunset:</strong> {policy.spec.deprecation.warnBeforeSunset}
                </Typography>
              )}
              <Typography variant="body2">
                <strong>Fail on warn:</strong>{' '}
                {policy.spec.contract?.failOnWarn ? 'Yes' : 'No'}
              </Typography>
            </Stack>
          ) : (
            <Typography variant="body2" color="text.secondary">
              No governance policy configured. The CLI will use its default
              settings or any flags passed directly in the pipeline.
              {policyRef && ` (looked for: "${policyRef}")`}
            </Typography>
          )}
        </div>
      </Stack>
    </InfoCard>
  );
}
