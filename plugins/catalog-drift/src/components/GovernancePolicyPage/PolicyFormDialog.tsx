import React, { useState } from 'react';
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  TextField,
  FormControlLabel,
  Checkbox,
  Stack,
  Typography,
  Alert,
} from '@mui/material';
import {
  CATALOG_DRIFT_API_VERSION,
  GOVERNANCE_POLICY_KIND,
  GovernancePolicyEntity,
} from '../../types';

interface Props {
  open: boolean;
  policy?: GovernancePolicyEntity;
  backstageUrl: string;
  onClose: () => void;
  onSaved: () => void;
}

/**
 * PolicyFormDialog handles create and edit for GovernancePolicy entities.
 * On save it POSTs a catalog-info YAML to the catalog API.
 */
export function PolicyFormDialog({ open, policy, backstageUrl, onClose, onSaved }: Props) {
  const isEdit = Boolean(policy);

  const [name, setName]                   = useState(policy?.metadata.name ?? '');
  const [namespace, setNamespace]         = useState(policy?.metadata.namespace ?? 'default');
  const [errorAfter, setErrorAfter]       = useState(policy?.spec.deprecation?.errorAfter ?? '');
  const [warnBefore, setWarnBefore]       = useState(policy?.spec.deprecation?.warnBeforeSunset ?? '');
  const [failOnWarn, setFailOnWarn]       = useState(policy?.spec.contract?.failOnWarn ?? false);
  const [saving, setSaving]               = useState(false);
  const [saveError, setSaveError]         = useState<string | undefined>();

  const handleSave = async () => {
    if (!name.trim()) {
      setSaveError('Policy name is required.');
      return;
    }

    const entity: GovernancePolicyEntity = {
      apiVersion: CATALOG_DRIFT_API_VERSION,
      kind: GOVERNANCE_POLICY_KIND,
      metadata: { name: name.trim(), namespace: namespace.trim() || 'default' },
      spec: {
        deprecation: {
          ...(errorAfter ? { errorAfter } : {}),
          ...(warnBefore ? { warnBeforeSunset: warnBefore } : {}),
        },
        contract: { failOnWarn },
      },
    };

    setSaving(true);
    setSaveError(undefined);
    try {
      const res = await fetch(`${backstageUrl}/api/catalog/entities`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
        body: JSON.stringify(entity),
      });
      if (!res.ok) {
        const body = await res.text();
        throw new Error(`Catalog API returned ${res.status}: ${body}`);
      }
      onSaved();
    } catch (e) {
      setSaveError((e as Error).message);
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle>{isEdit ? 'Edit Policy' : 'New Governance Policy'}</DialogTitle>
      <DialogContent>
        <Stack spacing={2} sx={{ mt: 1 }}>
          {saveError && <Alert severity="error">{saveError}</Alert>}

          <TextField
            label="Policy name"
            value={name}
            onChange={e => setName(e.target.value)}
            disabled={isEdit}
            helperText={
              isEdit
                ? 'Name cannot be changed after creation.'
                : 'Use "default" to apply to all components in a namespace.'
            }
            required
            fullWidth
          />

          <TextField
            label="Namespace"
            value={namespace}
            onChange={e => setNamespace(e.target.value)}
            helperText='Backstage namespace. Use "default" for a global policy.'
            disabled={isEdit}
            fullWidth
          />

          <Typography variant="subtitle2" sx={{ pt: 1 }}>
            Deprecation settings
          </Typography>

          <TextField
            label="Error after"
            value={errorAfter}
            onChange={e => setErrorAfter(e.target.value)}
            placeholder="e.g. 90d, 6m, 1y"
            helperText="Grace period from DeprecatedSince before deprecated usage becomes a pipeline error. Leave blank to never escalate."
            fullWidth
          />

          <TextField
            label="Warn before sunset"
            value={warnBefore}
            onChange={e => setWarnBefore(e.target.value)}
            placeholder="e.g. 30d"
            helperText="Start warning this far in advance of an API's sunset date."
            fullWidth
          />

          <Typography variant="subtitle2" sx={{ pt: 1 }}>
            Contract settings
          </Typography>

          <FormControlLabel
            control={
              <Checkbox
                checked={failOnWarn}
                onChange={e => setFailOnWarn(e.target.checked)}
              />
            }
            label="Fail pipeline on warnings (fail-on-warn)"
          />
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={saving}>Cancel</Button>
        <Button onClick={handleSave} variant="contained" disabled={saving}>
          {saving ? 'Saving…' : 'Save'}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
