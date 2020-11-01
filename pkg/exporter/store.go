package exporter

import (
	"regexp"

	"github.com/mvisonneau/gitlab-ci-pipelines-exporter/pkg/schemas"
	log "github.com/sirupsen/logrus"
)

func garbageCollectProjects() error {
	cfgUpdateLock.RLock()
	defer cfgUpdateLock.RUnlock()

	storedProjects, err := store.Projects()
	if err != nil {
		return err
	}

	// Loop through all configured projects
	for _, p := range config.Projects {
		delete(storedProjects, p.Key())
	}

	// Loop through what can be found from the wildcards
	for _, w := range config.Wildcards {
		foundProjects, err := gitlabClient.ListProjects(w)
		if err != nil {
			return err
		}

		for _, p := range foundProjects {
			delete(storedProjects, p.Key())
		}
	}

	log.WithFields(log.Fields{
		"projects-count": len(storedProjects),
	}).Info("found projects to garbage collect")

	for k, p := range storedProjects {
		if err = store.DelProject(k); err != nil {
			return err
		}

		log.WithFields(log.Fields{
			"project-name": p.Name,
		}).Info("deleted project from the store")
	}

	return nil
}

func garbageCollectEnvironments() error {
	cfgUpdateLock.RLock()
	defer cfgUpdateLock.RUnlock()

	storedEnvironments, err := store.Environments()
	if err != nil {
		return err
	}

	for k, env := range storedEnvironments {
		p := schemas.Project{
			Name: env.ProjectName,
		}

		projectExists, err := store.ProjectExists(p.Key())
		if err != nil {
			return err
		}

		// If the project does not exist anymore, delete the environment
		if !projectExists {
			if err = store.DelEnvironment(k); err != nil {
				return err
			}

			log.WithFields(log.Fields{
				"project-name":     env.ProjectName,
				"environment-name": env.Name,
				"reason":           "non-existent-project",
			}).Info("deleted environment from the store")
			continue
		}

		if err = store.GetProject(&p); err != nil {
			return err
		}

		// If the environment is not configured to be pulled anymore, delete it
		re := regexp.MustCompile(p.Pull.Environments.NameRegexp())
		if !re.MatchString(env.Name) {
			if err = store.DelEnvironment(k); err != nil {
				return err
			}

			log.WithFields(log.Fields{
				"project-name":     env.ProjectName,
				"environment-name": env.Name,
				"reason":           "environment-not-in-regexp",
			}).Info("deleted environment from the store")
			continue
		}

		// Check if the latest configuration of the project in store matches the environment one
		if env.OutputSparseStatusMetrics != p.OutputSparseStatusMetrics() ||
			env.TagsRegexp != p.Pull.Environments.TagsRegexp() {
			env.OutputSparseStatusMetrics = p.OutputSparseStatusMetrics()
			env.TagsRegexp = p.Pull.Environments.TagsRegexp()

			if err = store.SetEnvironment(env); err != nil {
				return err
			}

			log.WithFields(log.Fields{
				"project-name":     env.ProjectName,
				"environment-name": env.Name,
			}).Info("updated project ref, associated project configuration was not in sync")
		}
	}

	return nil
}

func garbageCollectRefs() error {
	cfgUpdateLock.RLock()
	defer cfgUpdateLock.RUnlock()

	storedRefs, err := store.Refs()
	if err != nil {
		return err
	}

	// storedProjects, err := store.Projects()
	// if err != nil {
	// 	return err
	// }

	// var existingRefs []schemas.RefKey
	// for k, p := range storedProjects {
	// 	branches, err := gitlabClient.GetProjectBranches(p.Name, p.Pull.Refs.Regexp())
	// 	if err != nil {
	// 		return err
	// 	}

	// 	for _, b := range branches {
	// 		ref := schemas.Ref{
	// 			Kind: ,
	// 		}
	// 	}
	// }

	for k, ref := range storedRefs {
		p := schemas.Project{Name: ref.ProjectName}
		projectExists, err := store.ProjectExists(p.Key())
		if err != nil {
			return err
		}

		// If the project does not exist anymore, delete the project ref
		if !projectExists {
			if err = store.DelRef(k); err != nil {
				return err
			}

			log.WithFields(log.Fields{
				"project-name": ref.ProjectName,
				"project-ref":  ref.Name,
				"reason":       "non-existent-project",
			}).Info("deleted project ref from the store")
			continue
		}

		if err = store.GetProject(&p); err != nil {
			return err
		}

		// If the ref is not configured to be pulled anymore, delete the project ref
		re := regexp.MustCompile(p.Pull.Refs.Regexp())
		if !re.MatchString(ref.Name) {
			if err = store.DelRef(k); err != nil {
				return err
			}

			log.WithFields(log.Fields{
				"project-name": ref.ProjectName,
				"project-ref":  ref.Name,
				"reason":       "ref-not-in-regexp",
			}).Info("deleted project ref from the store")
			continue
		}

		// Check if the latest configuration of the project in store matches the ref one
		if ref.OutputSparseStatusMetrics != p.OutputSparseStatusMetrics() ||
			ref.PullPipelineJobsEnabled != p.Pull.Pipeline.Jobs.Enabled() ||
			ref.PullPipelineVariablesEnabled != p.Pull.Pipeline.Variables.Enabled() ||
			ref.PullPipelineVariablesRegexp != p.Pull.Pipeline.Variables.Regexp() {
			ref.OutputSparseStatusMetrics = p.OutputSparseStatusMetrics()
			ref.PullPipelineJobsEnabled = p.Pull.Pipeline.Jobs.Enabled()
			ref.PullPipelineVariablesEnabled = p.Pull.Pipeline.Variables.Enabled()
			ref.PullPipelineVariablesRegexp = p.Pull.Pipeline.Variables.Regexp()
			if err = store.SetRef(ref); err != nil {
				return err
			}
			log.WithFields(log.Fields{
				"project-name": ref.ProjectName,
				"project-ref":  ref.Name,
			}).Info("updated project ref, associated project configuration was not in sync")
		}
	}

	return nil
}

func garbageCollectMetrics() error {
	cfgUpdateLock.RLock()
	defer cfgUpdateLock.RUnlock()

	storedEnvironments, err := store.Environments()
	if err != nil {
		return err
	}

	storedRefs, err := store.Refs()
	if err != nil {
		return err
	}

	storedMetrics, err := store.Metrics()
	if err != nil {
		return err
	}

	for k, m := range storedMetrics {
		// In order to save some memory space we chose to have to recompose
		// the Ref the metric belongs to
		metricLabelProject, metricLabelProjectExists := m.Labels["project"]
		metricLabelRef, metricLabelRefExists := m.Labels["ref"]
		metricLabelEnvironment, metricLabelEnvironmentExists := m.Labels["environment"]

		if !metricLabelProjectExists || (!metricLabelRefExists && !metricLabelEnvironmentExists) {
			if err = store.DelMetric(k); err != nil {
				return err
			}

			log.WithFields(log.Fields{
				"metric-kind":   m.Kind,
				"metric-labels": m.Labels,
				"reason":        "project-or-ref-and-environment-label-undefined",
			}).Info("deleted metric from the store")
		}

		if metricLabelRefExists {

			refKey := schemas.Ref{
				Kind:        schemas.RefKind(m.Labels["kind"]),
				ProjectName: metricLabelProject,
				Name:        metricLabelRef,
			}.Key()

			ref, refExists := storedRefs[refKey]

			// If the ref does not exist anymore, delete the metric
			if !refExists {
				if err = store.DelMetric(k); err != nil {
					return err
				}

				log.WithFields(log.Fields{
					"metric-kind":   m.Kind,
					"metric-labels": m.Labels,
					"reason":        "non-existent-ref",
				}).Info("deleted metric from the store")
				continue
			}

			// Check if the pulling of jobs related metrics has been disabled
			switch m.Kind {
			case schemas.MetricKindJobArtifactSizeBytes,
				schemas.MetricKindJobDurationSeconds,
				schemas.MetricKindJobID,
				schemas.MetricKindJobRunCount,
				schemas.MetricKindJobStatus,
				schemas.MetricKindJobTimestamp:

				if !ref.PullPipelineJobsEnabled {
					if err = store.DelMetric(k); err != nil {
						return err
					}

					log.WithFields(log.Fields{
						"metric-kind":   m.Kind,
						"metric-labels": m.Labels,
						"reason":        "jobs-metrics-disabled-on-project-ref",
					}).Info("deleted metric from the store")
					continue
				}

			default:
			}

			// Check if 'output sparse statuses metrics' has been enabled
			switch m.Kind {
			case schemas.MetricKindJobStatus,
				schemas.MetricKindStatus:

				if ref.OutputSparseStatusMetrics && m.Value != 1 {
					if err = store.DelMetric(k); err != nil {
						return err
					}

					log.WithFields(log.Fields{
						"metric-kind":   m.Kind,
						"metric-labels": m.Labels,
						"reason":        "output-sparse-metrics-enabled-on-project-ref",
					}).Info("deleted metric from the store")
					continue
				}

			default:
			}
		}

		if metricLabelEnvironmentExists {
			envKey := schemas.Environment{
				ProjectName: metricLabelProject,
				Name:        metricLabelEnvironment,
			}.Key()

			_, envExists := storedEnvironments[envKey]

			// If the project ref does not exist anymore, delete the metric
			if !envExists {
				if err = store.DelMetric(k); err != nil {
					return err
				}

				log.WithFields(log.Fields{
					"metric-kind":   m.Kind,
					"metric-labels": m.Labels,
					"reason":        "non-existent-environment",
				}).Info("deleted metric from the store")
				continue
			}
		}
	}

	return nil
}

func metricLogFields(m schemas.Metric) log.Fields {
	return log.Fields{
		"metric-kind":   m.Kind,
		"metric-labels": m.Labels,
	}
}

func storeGetMetric(m *schemas.Metric) {
	cfgUpdateLock.RLock()
	defer cfgUpdateLock.RUnlock()

	if err := store.GetMetric(m); err != nil {
		log.WithFields(
			metricLogFields(*m),
		).WithField(
			"error", err.Error(),
		).Errorf("reading metric from the store")
	}
}

func storeSetMetric(m schemas.Metric) {
	cfgUpdateLock.RLock()
	defer cfgUpdateLock.RUnlock()

	if err := store.SetMetric(m); err != nil {
		log.WithFields(
			metricLogFields(m),
		).WithField(
			"error", err.Error(),
		).Errorf("writing metric in the store")
	}
}

func storeDelMetric(m schemas.Metric) {
	cfgUpdateLock.RLock()
	defer cfgUpdateLock.RUnlock()

	if err := store.DelMetric(m.Key()); err != nil {
		log.WithFields(
			metricLogFields(m),
		).WithField(
			"error", err.Error(),
		).Errorf("deleting metric from the store")
	}
}
