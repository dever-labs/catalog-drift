/**
 * Types that mirror the GovernancePolicy custom entity kind understood by
 * catalog-drift. These are used both in the plugin UI and when creating
 * entities programmatically via the Backstage catalog API.
 */

/** The API version for catalog-drift custom entities. */
export const CATALOG_DRIFT_API_VERSION = 'catalog-drift.io/v1alpha1';

/** The entity kind for governance policies. */
export const GOVERNANCE_POLICY_KIND = 'GovernancePolicy';

/** Annotation keys set on API and Component entities to opt into a policy. */
export const ANNOTATION_GOVERNANCE_POLICY = 'catalog-drift/governance-policy';
export const ANNOTATION_DEPRECATED_SINCE = 'catalog-drift/deprecated-since';
export const ANNOTATION_SUNSET_DATE = 'catalog-drift/sunset-date';
export const ANNOTATION_DEPRECATION_MESSAGE = 'catalog-drift/deprecation-message';
export const ANNOTATION_SUCCESSOR = 'catalog-drift/successor';

/**
 * GovernancePolicySpec defines how the catalog-drift CLI treats findings for
 * components governed by this policy.
 */
export interface GovernancePolicySpec {
  /**
   * deprecation controls when deprecated-usage findings escalate from warning
   * to error.
   */
  deprecation: {
    /**
     * errorAfter is the grace period (e.g. "90d", "6m") measured from an API's
     * `catalog-drift/deprecated-since` annotation. Once elapsed, every call to
     * that API becomes an error in the pipeline.
     *
     * Leave empty to keep deprecated-usage findings as warnings indefinitely.
     */
    errorAfter?: string;

    /**
     * warnBeforeSunset starts emitting warnings this far in advance of the API's
     * `catalog-drift/sunset-date` annotation, even if the API hasn't been marked
     * deprecated yet.
     */
    warnBeforeSunset?: string;
  };

  /**
   * contract controls how contract-checking findings are handled.
   */
  contract: {
    /**
     * failOnWarn makes the pipeline exit non-zero when any warning-severity
     * finding is reported, not just errors.
     */
    failOnWarn?: boolean;
  };
}

/** Full GovernancePolicy entity as stored in the Backstage catalog. */
export interface GovernancePolicyEntity {
  apiVersion: typeof CATALOG_DRIFT_API_VERSION;
  kind: typeof GOVERNANCE_POLICY_KIND;
  metadata: {
    name: string;
    namespace: string;
    title?: string;
    description?: string;
    annotations?: Record<string, string>;
    tags?: string[];
  };
  spec: GovernancePolicySpec;
}
