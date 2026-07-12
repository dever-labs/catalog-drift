import React, { useEffect, useState, useCallback } from 'react';
import {
  Content,
  ContentHeader,
  SupportButton,
  Table,
  TableColumn,
  Progress,
  ResponseErrorPanel,
  Link,
} from '@backstage/core-components';
import { useApi, configApiRef } from '@backstage/core-plugin-api';
import { GovernancePolicyEntity } from '../../types';
import { PolicyFormDialog } from './PolicyFormDialog';

const columns: TableColumn<GovernancePolicyEntity>[] = [
  {
    title: 'Name',
    field: 'metadata.name',
    render: row => (
      <Link to={`../catalog-drift/${row.metadata.namespace}/${row.metadata.name}`}>
        {row.metadata.name}
      </Link>
    ),
  },
  { title: 'Namespace', field: 'metadata.namespace' },
  {
    title: 'Error after',
    render: row => row.spec.deprecation?.errorAfter ?? '—',
  },
  {
    title: 'Warn before sunset',
    render: row => row.spec.deprecation?.warnBeforeSunset ?? '—',
  },
  {
    title: 'Fail on warn',
    render: row => (row.spec.contract?.failOnWarn ? 'Yes' : 'No'),
  },
];

/**
 * GovernancePolicyPage lists all GovernancePolicy entities in the catalog and
 * allows administrators to create new policies or edit existing ones.
 *
 * Each policy is stored as a catalog entity of kind GovernancePolicy
 * (apiVersion: catalog-drift.io/v1alpha1). Components reference a policy via
 * the `catalog-drift/governance-policy` annotation.
 */
export function GovernancePolicyPage() {
  const configApi = useApi(configApiRef);
  const backstageUrl = configApi.getString('backend.baseUrl');

  const [policies, setPolicies] = useState<GovernancePolicyEntity[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | undefined>();
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editing, setEditing] = useState<GovernancePolicyEntity | undefined>();

  const fetchPolicies = useCallback(async () => {
    setLoading(true);
    try {
      const res = await fetch(
        `${backstageUrl}/api/catalog/entities?filter=kind=GovernancePolicy`,
        { headers: { Accept: 'application/json' } },
      );
      if (!res.ok) throw new Error(`Catalog API returned ${res.status}`);
      const data: GovernancePolicyEntity[] = await res.json();
      setPolicies(data);
    } catch (e) {
      setError(e as Error);
    } finally {
      setLoading(false);
    }
  }, [backstageUrl]);

  useEffect(() => { fetchPolicies(); }, [fetchPolicies]);

  if (loading) return <Progress />;
  if (error)   return <ResponseErrorPanel error={error} />;

  return (
    <Content>
      <ContentHeader title="API Governance Policies">
        <SupportButton>
          Governance policies control how the catalog-drift CLI treats deprecated
          API usage in your pipelines. Each policy can set a grace period before
          deprecated usage becomes a pipeline error, and whether warnings cause
          pipeline failures.
        </SupportButton>
      </ContentHeader>

      <Table
        options={{ paging: false }}
        data={policies}
        columns={columns}
        title="Policies"
        actions={[
          {
            icon: 'add',
            tooltip: 'New policy',
            isFreeAction: true,
            onClick: () => { setEditing(undefined); setDialogOpen(true); },
          },
          {
            icon: 'edit',
            tooltip: 'Edit policy',
            onClick: (_, row) => {
              setEditing(row as GovernancePolicyEntity);
              setDialogOpen(true);
            },
          },
        ]}
      />

      {dialogOpen && (
        <PolicyFormDialog
          open={dialogOpen}
          policy={editing}
          backstageUrl={backstageUrl}
          onClose={() => setDialogOpen(false)}
          onSaved={() => { setDialogOpen(false); fetchPolicies(); }}
        />
      )}
    </Content>
  );
}
